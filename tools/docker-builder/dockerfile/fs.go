package dockerfile

import (
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing/fstest"
	"time"
)

type mappedFS struct {
	root fs.FS
	// Map of real file -> virtual file
	references map[string][]string
	// Map of virtual file -> real file
	reverse map[string]string
}

func (mfs mappedFS) Open(name string) (fs.File, error) {
	log.Errorf("howardjohn: open %v", name)
	fs := map[string]struct{}{}
	for k := range mfs.reverse {
		rel, err := filepath.Rel(name, "."+k)
		if err != nil {
			continue
		}
		fs[filepath.SplitList(rel)[0]] = struct{}{}
		log.Errorf("howardjohn: %v/%v: %v, %v", k, name, rel, err)
	}
	//if m, f := mfs.references[name]; f {
	//	log.Errorf("howardjohn: remap to %v", m)
	//	return mfs.root.Open(m)
	//}
	// First it opens this up,
	// but then calls ReadDir if its a dir. So we just need to say its a dir
	return NewDir(name), nil
}

var _ fs.FS = mappedFS{}

func MappedFS(root fs.FS, references map[string][]string) fs.FS {
	files := map[string]*fstest.MapFile{}
	mapfs := fstest.MapFS{}
	for k := range references {
		log.Errorf("howardjohn: open %v", k)
		fi, err := root.Open(k)
		if err != nil {
			panic(err.Error())
		}
		s, err := fi.Stat()
		if err != nil {
			panic(err.Error())
		}
		b, err := ioutil.ReadAll(fi)
		if err != nil {
			panic(err.Error())
		}
		files[k] = &fstest.MapFile{
			Data:    b,
			Mode:    s.Mode(),
			ModTime: s.ModTime(),
			Sys:     s.Sys(),
		}
	}
	for k, vs := range references {
		for _, v := range vs {
			mapfs[strings.TrimPrefix(v, "/")] = files[k]
			log.Errorf("howardjohn: write %v", v)
		}
	}
	return mapfs
}

type MockDir struct {
	isDir   bool
	modTime time.Time
	mode    fs.FileMode
	name    string
	size    int64
	sys     interface{}
}

var (
	_ fs.File     = &MockDir{}
	_ fs.FileInfo = &MockDir{}
)

func (m *MockDir) Name() string {
	return m.name
}

func (m *MockDir) IsDir() bool {
	return m.isDir
}

func (mf *MockDir) Info() (fs.FileInfo, error) {
	return mf.Stat()
}

func (mf *MockDir) Stat() (fs.FileInfo, error) {
	return mf, nil
}

func (m *MockDir) Size() int64 {
	return m.size
}

func (m *MockDir) Mode() os.FileMode {
	return m.mode
}

func (m *MockDir) ModTime() time.Time {
	return m.modTime
}

func (m *MockDir) Sys() interface{} {
	return m.sys
}

func (m *MockDir) Type() fs.FileMode {
	return m.Mode().Type()
}

func (mf *MockDir) Read(p []byte) (int, error) {
	panic("not implemented read on " + mf.name)
}

func (mf *MockDir) Close() error {
	return nil
}

func NewDir(name string) *MockDir {
	return &MockDir{
		isDir: true,
		mode:  os.ModeDir,
		name:  name,
	}
}
