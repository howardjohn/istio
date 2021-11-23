package builder

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"istio.io/pkg/log"
)

type Args struct {
	// Name is optional, for logging
	Name string

	Env        map[string]string
	User       string
	WorkDir    string
	Entrypoint []string

	// Base image to use
	Base string

	// Files contains all files, mapping destination path -> source path
	Files map[string]string
	// FilesBase is the base path for absolute paths in Files
	FilesBase string
}

var (
	bases   = map[string]v1.Image{}
	basesMu sync.RWMutex
)

func WarmBase(baseImages ...string) {
	basesMu.Lock()
	wg := sync.WaitGroup{}
	wg.Add(len(baseImages))
	go func() {
		wg.Wait()
		basesMu.Unlock()
	}()

	t0 := time.Now()
	for _, b := range baseImages {
		b := b
		go func() {
			ref, err := name.ParseReference(b)
			if err != nil {
				log.WithLabels("image", b).Warnf("base failed: %v", err)
				return
			}
			bi, err := remote.Image(ref, remote.WithProgress(CreateProgress(fmt.Sprintf("base %v", ref))))
			if err != nil {
				log.WithLabels("image", b).Warnf("base failed: %v", err)
				return
			}
			log.WithLabels("image", b, "step", time.Since(t0)).Infof("base loaded")
			bases[b] = bi
			wg.Done()
		}()
	}
}

func ByteCount(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

func Build(args Args, dests []string) error {
	t0 := time.Now()
	lt := t0
	trace := func(d ...interface{}) {
		log.WithLabels("image", args.Name, "total", time.Since(t0), "step", time.Since(lt)).Infof(d...)
		lt = time.Now()
	}
	if len(dests) == 0 {
		return fmt.Errorf("dest required")
	}

	baseImage := empty.Image
	if args.Base != "" {
		basesMu.RLock()
		baseImage = bases[args.Base]
		basesMu.RUnlock()
	}
	if baseImage == nil {
		log.Warnf("on demand loading base image %q", args.Base)
		ref, err := name.ParseReference(args.Base)
		if err != nil {
			return err
		}
		bi, err := remote.Image(ref, remote.WithProgress(CreateProgress(fmt.Sprintf("base %v", ref))))
		if err != nil {
			return err
		}
		baseImage = bi
	}
	trace("create base")

	cfgFile, err := baseImage.ConfigFile()
	if err != nil {
		return err
	}

	trace("base config")

	cfg := cfgFile.Config
	for k, v := range args.Env {
		cfg.Env = append(cfg.Env, fmt.Sprintf("%v=%v", k, v))
	}
	if args.User != "" {
		cfg.User = args.User
	}
	if len(args.Entrypoint) > 0 {
		cfg.Entrypoint = args.Entrypoint
	}
	if args.WorkDir != "" {
		cfg.WorkingDir = args.WorkDir
	}

	updated, err := mutate.Config(baseImage, cfg)
	if err != nil {
		return err
	}
	trace("config")

	// Pre-allocated 100mb
	// TODO: cache the size of images, use exactish size
	buf := bytes.NewBuffer(make([]byte, 0, 100*1024*1024))
	if err := WriteArchiveFromFiles(args.FilesBase, args.Files, buf); err != nil {
		return err
	}
	sz := ByteCount(int64(buf.Len()))
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewReader(buf.Bytes())), nil
	}, tarball.WithCompressionLevel(gzip.NoCompression))
	if err != nil {
		return err
	}
	trace("read layer of size %v", sz)

	files, err := mutate.AppendLayers(updated, l)
	if err != nil {
		return err
	}

	trace("layer")

	// Write Remote

	// MultiWrite takes a Reference -> Taggable, but won't handle writing to multiple Repos. So we keep
	// a map of Repository -> MultiWrite args.
	remoteTargets := map[name.Repository]map[name.Reference]remote.Taggable{}

	for _, dest := range dests {
		destRef, err := name.ParseReference(dest)
		if err != nil {
			return err
		}
		repo := destRef.Context()
		if remoteTargets[repo] == nil {
			remoteTargets[repo] = map[name.Reference]remote.Taggable{}
		}
		remoteTargets[repo][destRef] = files
	}

	for repo, mw := range remoteTargets {
		// TOOD: do we need to do anything to mount the base images?
		prog := CreateProgress(fmt.Sprintf("upload %v", repo.String()))
		if err := remote.MultiWrite(mw, remote.WithProgress(prog), remote.WithAuthFromKeychain(authn.DefaultKeychain)); err != nil {
			return err
		}
		trace("upload %v", repo.String())
	}

	return nil
}

func CreateProgress(name string) chan v1.Update {
	updates := make(chan v1.Update, 1000)
	go func() {
		lastLog := time.Time{}
		for {
			select {
			case u, ok := <-updates:
				if !ok {
					return
				}
				if time.Since(lastLog) < time.Second && !(u.Total == u.Complete) {
					// Limit to 1 log per image per second, unless its the final log
					continue
				}
				if u.Total == 0 {
					continue
				}
				lastLog = time.Now()
				log.WithLabels("action", name).Infof("progress %s/%s", ByteCount(u.Complete), ByteCount(u.Total))
			}
		}
	}()
	return updates
}
