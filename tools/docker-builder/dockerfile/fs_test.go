package dockerfile

import (
	"os"
	"testing"
	"testing/fstest"
)

func TestFS(t *testing.T) {
	fstest.MapFS{}
	fs := MappedFS(os.DirFS("."), nil)
	if err := fstest.TestFS(fs, "fs_test.go"); err != nil {
		t.Fatal(err)
	}
}
