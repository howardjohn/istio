package john

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/manifests"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/yml"
)

type Manifest struct {
	*unstructured.Unstructured
	Content string
}

func ManifestFromYaml(y []byte) (Manifest, error) {
	us := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(y, us); err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(y),
	}, nil
}

func ManifestFromJson(j []byte) (Manifest, error) {
	us := &unstructured.Unstructured{}
	if err := json.Unmarshal(j, us); err != nil {
		return Manifest{}, err
	}
	yml, err := yaml.Marshal(us)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(yml),
	}, nil
}

func ManifestFromObject(us *unstructured.Unstructured) (Manifest, error) {
	c, err := yaml.Marshal(us)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Unstructured: us,
		Content:      string(c),
	}, nil
}

func (m Manifest) Hash() string {
	k := m.GroupVersionKind().Kind
	switch m.GroupVersionKind().Kind {
	case "ClusterRole", "ClusterRoleBinding":
		return k + ":" + m.GetName()
	}
	return k + ":" + m.GetNamespace() + ":" + m.GetName()
}

func Render(spec ComponentSpec, comp Component, iop Map) ([]Manifest, error) {
	vals, _ := iop.GetPathMap("spec.values")
	installPackagePath := TryGetPathAs[string](iop, "spec.installPackagePath")
	f := manifests.BuiltinOrDir(installPackagePath)
	path := filepath.Join("charts", comp.HelmSubdir)
	chrt, err := loadChart(f, path)
	if err != nil {
		return nil, err
	}

	output, err := renderChart(spec, vals, chrt, nil, nil)
	if err != nil {
		return nil, err
	}
	return ParseManifests(output)
}

func ParseManifests(output []string) ([]Manifest, error) {
	res := make([]Manifest, 0, len(output))
	for _, m := range output {
		mf, err := ManifestFromJson([]byte(m))
		if err != nil {
			return nil, err
		}
		if mf.GetObjectKind().GroupVersionKind().Kind == "" {
			// This is not an object. Could be empty template, comments only, etc
			continue
		}
		res = append(res, mf)
	}
	return res, nil
}

// TemplateFilterFunc filters templates to render by their file name
type TemplateFilterFunc func(string) bool

// renderChart renders the given chart with the given values and returns the resulting YAML manifest string.
func renderChart(spec ComponentSpec, values Map, chrt *chart.Chart, filterFunc TemplateFilterFunc, version *version.Info) ([]string, error) {
	options := chartutil.ReleaseOptions{
		Name:      "istio", // TODO: this should probably have been the component name. However, its a bit late to change it.
		Namespace: spec.Namespace,
	}

	caps := *chartutil.DefaultCapabilities

	// overwrite helm default capabilities
	operatorVersion, _ := chartutil.ParseKubeVersion("1." + strconv.Itoa(k8sversion.MinK8SVersion) + ".0")
	caps.KubeVersion = *operatorVersion

	if version != nil {
		caps.KubeVersion = chartutil.KubeVersion{
			Version: version.GitVersion,
			Major:   version.Major,
			Minor:   version.Minor,
		}
	}
	vals, err := chartutil.ToRenderValues(chrt, values, options, &caps)
	if err != nil {
		return nil, err
	}

	if filterFunc != nil {
		filteredTemplates := []*chart.File{}
		for _, t := range chrt.Templates {
			// Always include required templates that do not produce any output
			if filterFunc(t.Name) ||
				strings.HasSuffix(t.Name, ".tpl") ||
				t.Name == "templates/zzz_profile.yaml" ||
				t.Name == "templates/zzy_descope_legacy.yaml" {
				filteredTemplates = append(filteredTemplates, t)
			}
		}
		chrt.Templates = filteredTemplates
	}

	files, err := engine.Render(chrt, vals)
	if err != nil {
		return nil, err
	}

	crdFiles := chrt.CRDObjects()
	if chrt.Metadata.Name == "base" {
		enableIstioConfigCRDs, ok := GetPathAs[bool](values, "base.enableIstioConfigCRDs")
		if ok && !enableIstioConfigCRDs {
			crdFiles = []chart.CRD{}
		}
	}

	// Create sorted array of keys to iterate over, to stabilize the order of the rendered templates
	keys := make([]string, 0, len(files))
	for k := range files {
		if strings.HasSuffix(k, NotesFileNameSuffix) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	results := make([]string, 0, len(keys))
	for _, k := range keys {
		results = append(results, yml.SplitString(files[k])...)
	}

	// Sort crd files by name to ensure stable manifest output
	slices.SortBy(crdFiles, func(a chart.CRD) string {
		return a.Name
	})
	for _, crd := range crdFiles {
		results = append(results, yml.SplitString(string(crd.File.Data))...)
	}

	return results, nil
}

const (
	// NotesFileNameSuffix is the file name suffix for helm notes.
	// see https://helm.sh/docs/chart_template_guide/notes_files/
	NotesFileNameSuffix = ".txt"
)

// loadChart reads a chart from the filesystem. This is like loader.LoadDir but allows a fs.FS.
func loadChart(f fs.FS, root string) (*chart.Chart, error) {
	fnames, err := getFilesRecursive(f, root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("component does not exist")
		}
		return nil, fmt.Errorf("list files: %v", err)
	}
	var bfs []*loader.BufferedFile
	for _, fname := range fnames {
		b, err := fs.ReadFile(f, fname)
		if err != nil {
			return nil, fmt.Errorf("read file: %v", err)
		}
		// Helm expects unix / separator, but on windows this will be \
		name := strings.ReplaceAll(stripPrefix(fname, root), string(filepath.Separator), "/")
		bf := &loader.BufferedFile{
			Name: name,
			Data: b,
		}
		bfs = append(bfs, bf)
	}

	return loader.LoadFiles(bfs)
}

// stripPrefix removes the given prefix from prefix.
func stripPrefix(path, prefix string) string {
	pl := len(strings.Split(prefix, "/"))
	pv := strings.Split(path, "/")
	return strings.Join(pv[pl:], "/")
}

func getFilesRecursive(f fs.FS, root string) ([]string, error) {
	res := []string{}
	err := fs.WalkDir(f, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		res = append(res, path)
		return nil
	})
	return res, err
}
