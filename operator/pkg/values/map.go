package values

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/ptr"
)

// Map is a wrapper around an untyped map. This is used throughout the operator codebase to provide generic access.
// While un-intuitive, as generally strong typing is preferred, this solves a number of problems:
// A large portion of the codebase is dealing with inherently unstructured input. The core type, the Helm values, is already
// an untyped value.
//
// For example, while we do have a value_types.proto representation, this isn't actually appropriate for direct usage. For example, we allow
// passing `unvalidatedValues` which is a completely opaque blob. We also allow many types to be both string or ints, for instance, which makes usage awkward.
// Really this is useful for *validation* but not usage throughout the codebase.
// Historically, there were attempts to use the direct typed value. In practice, what we ended up doing is converting to/from
// the typed struct, protobuf.Struct, JSON/YAML, and an unstructured Map, depending on the needs of the current codebase.
//
// Some other problems with attempting to use typed structs:
//   - There is a mix of golang protobuf (Istio) and gogo protobuf (Kubernetes types) which have poor interactions
//   - Typed structs lose context on what was explicitly set by a user vs the zero value (without taking care at every point, and making more painful
//     struct definitions). For instance, `pilot.enabled=false` is very different from just not setting `enabled`, which defaults to 'true'.
//     Some of these types also come from others (Kubernetes), which we don't control.
//   - A large portion of the code is dynamically getting or setting values, like applying `--set values.foo.bar=baz`. These are MUCH easier
//     to do on a Map than on a struct which requires complex reflection.
type Map map[string]any

// MapFromJson constructs a Map from JSON
func MapFromJson(input []byte) (Map, error) {
	m := make(Map)
	err := json.Unmarshal(input, &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// MapFromYaml constructs a Map from YAML
func MapFromYaml(input []byte) (Map, error) {
	m := make(Map)
	err := yaml.Unmarshal(input, &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// MakeMap is a helper to construct a map that has a single value, nested under some set of paths.
// For example, `MakeMap(1, "a", "b") = Map{"a": Map{"b": 1}}`
func MakeMap(contents any, path ...string) Map {
	ret := Map{path[len(path)-1]: contents}
	for i := len(path) - 2; i >= 0; i-- {
		ret = Map{path[i]: ret}
	}
	return ret
}

// AsMap casts a value to a Map, if possible.
func AsMap(cur any) (Map, bool) {
	if m, ok := cur.(Map); ok {
		return m, true
	}
	if m, ok := cur.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

// MustAsMap casts a value to a Map; if the value is not a map, it will panic..
func MustAsMap(cur any) Map {
	m, ok := AsMap(cur)
	if !ok {
		if !reflect.ValueOf(cur).IsValid() {
			return Map{}
		}
		panic(fmt.Sprintf("not a map, got %T: %v %v", cur, cur, reflect.ValueOf(cur).Kind()))
	}
	return m
}

// JSON serializes a Map to a JSON string.
func (m Map) JSON() string {
	b, err := json.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("json Marshal: %v", err))
	}
	return string(b)
}

// YAML serializes a Map to a YAML string.
func (m Map) YAML() string {
	b, err := yaml.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("yaml Marshal: %v", err))
	}
	return string(b)
}

// MergeFrom does a key-wise merge between the current map and the passed in map.
// The other map has precedence, and the result will modify the current map.
func (m Map) MergeFrom(other Map) {
	for k, v := range other {
		// Might be a Map or map, possibly recurse
		if vm, ok := v.(Map); ok {
			v = (map[string]any)(vm)
		}
		if v, ok := v.(map[string]any); ok {
			// It's a map...
			if bv, ok := m[k]; ok {
				// And the base map has the same key
				if bv, ok := bv.(map[string]any); ok {
					// And it is a map in the base as well
					Map(bv).MergeFrom(v)
					continue
				}
			}
		}
		// Simple overwrite
		m[k] = v
	}
}

// SetPaths applies values from input like `key.subkey=val`
func (m Map) SetPaths(paths ...string) error {
	for _, sf := range paths {
		p, v := getPV(sf)
		// input value type is always string, transform it to correct type before setting.
		var val any = v
		if !isAlwaysString(p) {
			val = parseValue(v)
		}
		if err := m.SetPath(p, val); err != nil {
			return err
		}
	}
	return nil
}

// SetPath applies values from a path like `key.subkey`, `key.[0].var`, or `key.[name:foo]`.
func (m Map) SetPath(paths string, value any) error {
	path, err := splitPath(paths)
	if err != nil {
		return err
	}
	base := m
	if err := setPathRecurse(base, path, value); err != nil {
		return err
	}
	return nil
}

// SetSpecPaths applies values from input like `key.subkey=val`, and applies them under 'spec'
func (m Map) SetSpecPaths(paths ...string) error {
	for _, path := range paths {
		if err := m.SetPaths("spec." + path); err != nil {
			return err
		}
	}
	return nil
}

// GetPathAs is a helper function to get a patch value and cast it to a specified type.
// If the path is not found, or the cast fails, false is returned.
func GetPathAs[T any](m Map, name string) (T, bool) {
	v, ok := m.GetPath(name)
	if !ok {
		return ptr.Empty[T](), false
	}
	t, ok := v.(T)
	return t, ok
}

// TryGetPathAs is a helper function to get a patch value and cast it to a specified type.
// If the path is not found, or the cast fails, the zero value is returned.
func TryGetPathAs[T any](m Map, name string) T {
	v, ok := m.GetPath(name)
	if !ok {
		return ptr.Empty[T]()
	}
	t, ok := v.(T)
	return t
}

// GetPath gets values from input like `key.subkey`, `key.[0].var`, or `key.[name:foo]`.
func (m Map) GetPath(name string) (any, bool) {
	cur := any(m)

	paths, err := splitPath(name)
	if err != nil {
		return nil, false
	}
	for _, n := range paths {
		if idx, ok := extractIndex(n); ok {
			a, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			if idx >= 0 && idx < len(a) {
				cur = a[idx]
			} else {
				return nil, false
			}
		} else if k, v, ok := extractKV(n); ok {
			a, ok := cur.([]any)
			if !ok {
				return nil, false
			}
			index := -1
			for idx, cm := range a {
				if MustAsMap(cm)[k] == v {
					index = idx
					break
				}
			}
			if index == -1 {
				return nil, false
			}
			cur = a[idx]
		} else {
			cm, ok := AsMap(cur)
			if !ok {
				return nil, false
			}
			sub, ok := cm[n]
			if !ok {
				return nil, false
			}
			cur = sub
		}
	}

	if p, ok := cur.(*any); ok {
		return *p, true
	}
	return cur, true
}

// GetPathMap gets values from input like `key.subkey`
func (m Map) GetPathMap(name string) (Map, bool) {
	cur := m

	for _, n := range parsePath(name) {
		sub, ok := tableLookup(cur, n)
		if !ok {
			return nil, false
		}
		cur = sub
	}
	return cur, true
}

// DeepClone performs a deep clone of the map
func (m Map) DeepClone() Map {
	// TODO: More efficient way?
	res, err := ConvertMap[Map](m)
	if err != nil {
		panic("deep clone should not fail")
	}
	return res
}

// GetPathString is a helper around TryGetPathAs[string] to allow usage as a method (otherwise impossible with generics)
func (m Map) GetPathString(s string) string {
	return TryGetPathAs[string](m, s)
}

// GetPathStringOr is a helper around TryGetPathAs[string] to allow usage as a method (otherwise impossible with generics),
// with an allowance for a default value if it is not found/not set.
func (m Map) GetPathStringOr(s string, def string) string {
	return ptr.NonEmptyOrDefault(m.GetPathString(s), def)
}

// GetPathBool is a helper around TryGetPathAs[bool] to allow usage as a method (otherwise impossible with generics)
func (m Map) GetPathBool(s string) bool {
	return TryGetPathAs[bool](m, s)
}

// ConvertMap translates a Map to a T, via JSON
func ConvertMap[T any](m Map) (T, error) {
	return fromJson[T]([]byte(m.JSON()))
}

func setPathRecurse(base map[string]any, paths []string, value any) error {
	seg := paths[0]
	last := len(paths) == 1
	nextIsArray := len(paths) >= 2 && strings.HasPrefix(paths[1], "[")
	if nextIsArray {
		last = len(paths) == 2
		// Find or create target list
		if _, f := base[seg]; !f {
			base[seg] = []any{}
		}
		var index int
		if k, v, ok := extractKV(paths[1]); ok {
			index = -1
			for idx, cm := range base[seg].([]any) {
				if MustAsMap(cm)[k] == v {
					index = idx
					break
				}
			}
			if index == -1 {
				return fmt.Errorf("element %v not found", paths[1])
			}
		} else if idx, ok := extractIndex(paths[1]); ok {
			index = idx
		} else {
			return fmt.Errorf("unknown segment %v", paths[1])
		}
		l := base[seg].([]any)
		if index < 0 || index >= len(l) {
			// Index is greater, we need to append
			if last {
				l = append(l, value)
			} else {
				nm := Map{}
				if err := setPathRecurse(nm, paths[2:], value); err != nil {
					return err
				}
				l = append(l, nm)
			}
			base[seg] = l
		} else {
			v := MustAsMap(l[index])
			if err := setPathRecurse(v, paths[2:], value); err != nil {
				return err
			}
			l[index] = v
		}
	} else {
		// This is a simple key traverse
		// Find or create the target
		// Create if needed
		if _, f := base[seg]; !f {
			base[seg] = map[string]any{}
		}
		if last {
			base[seg] = value
		} else {
			return setPathRecurse(MustAsMap(base[seg]), paths[1:], value)
		}
	}
	return nil
}

// GetValueForSetFlag parses the passed set flags which have format key=value and if any set the given path,
// returns the corresponding value, otherwise returns the empty string. setFlags must have valid format.
func GetValueForSetFlag(setFlags []string, path string) string {
	ret := ""
	for _, sf := range setFlags {
		p, v := getPV(sf)
		if p == path {
			ret = v
		}
		// if set multiple times, return last set value
	}
	return ret
}

func fromJson[T any](overlay []byte) (T, error) {
	v := new(T)
	err := json.Unmarshal(overlay, &v)
	if err != nil {
		return ptr.Empty[T](), err
	}
	return *v, nil
}

func tableLookup(v Map, simple string) (Map, bool) {
	v2, ok := v[simple]
	if !ok {
		return nil, false
	}
	if vv, ok := v2.(map[string]interface{}); ok {
		return vv, true
	}

	// This catches a case where a value is of type Values, but doesn't (for some
	// reason) match the map[string]interface{}. This has been observed in the
	// wild, and might be a result of a nil map of type Values.
	if vv, ok := v2.(Map); ok {
		return vv, true
	}

	return nil, false
}

func parsePath(key string) []string { return strings.Split(key, ".") }

// alwaysString represents types that should always be decoded as strings
// TODO: this could be automatically derived from the value_types.proto?
var alwaysString = []string{
	"spec.values.compatibilityVersion",
	"spec.meshConfig.defaultConfig.proxyMetadata.",
	"spec.values.meshConfig.defaultConfig.proxyMetadata.",
	"spec.compatibilityVersion",
}

func isAlwaysString(s string) bool {
	for _, a := range alwaysString {
		if strings.HasPrefix(s, a) {
			return true
		}
	}
	return false
}

// parseValue parses string into a value
func parseValue(valueStr string) any {
	var value any
	if v, err := strconv.Atoi(valueStr); err == nil {
		value = v
	} else if v, err := strconv.ParseFloat(valueStr, 64); err == nil {
		value = v
	} else if v, err := strconv.ParseBool(valueStr); err == nil {
		value = v
	} else {
		value = strings.ReplaceAll(valueStr, "\\,", ",")
	}
	return value
}

func splitPath(in string) ([]string, error) {
	segments := []string{}
	for {
		if strings.HasPrefix(in, "[") {
			idx := strings.Index(in, "]")
			if idx == -1 {
				return nil, fmt.Errorf("unclosed segment")
			}
			segments = append(segments, in[:idx+1])
			if len(in) <= idx+1 {
				return segments, nil
			}
			in = in[idx+2:]
		} else {
			idx := strings.Index(in, ".")
			if idx == -1 {
				segments = append(segments, in)
				return segments, nil
			}
			segments = append(segments, in[:idx])
			in = in[idx+1:]
		}
	}
}

func extractIndex(seg string) (int, bool) {
	if !strings.HasPrefix(seg, "[") || !strings.HasSuffix(seg, "]") {
		return 0, false
	}
	sanitized := seg[1 : len(seg)-1]
	v, err := strconv.Atoi(sanitized)
	if err != nil {
		return 0, false
	}
	return v, true
}

func extractKV(seg string) (string, string, bool) {
	if !strings.HasPrefix(seg, "[") || !strings.HasSuffix(seg, "]") {
		return "", "", false
	}
	sanitized := seg[1 : len(seg)-1]
	return strings.Cut(sanitized, ":")
}

// getPV returns the path and value components for the given set flag string, which must be in path=value format.
func getPV(setFlag string) (path string, value string) {
	pv := strings.Split(setFlag, "=")
	if len(pv) != 2 {
		return setFlag, ""
	}
	path, value = strings.TrimSpace(pv[0]), strings.TrimSpace(pv[1])
	return
}
