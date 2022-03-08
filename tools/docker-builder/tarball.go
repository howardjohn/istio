package main

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Writes a raw TAR archive to out, given an fs.FS.
func WriteArchiveFromFS(base string, fsys fs.FS, out io.Writer, sourceDateEpoch time.Time) error {
	tw := tar.NewWriter(out)
	defer tw.Close()

	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink == os.ModeSymlink {
			var link string
			// fs.FS does not implement readlink, so we have this hack for now.
			if link, err = os.Readlink(filepath.Join(base, path)); err != nil {
				return err
			}
			// Resolve the link
			if info, err = os.Stat(link); err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		// work around some weirdness, without this we wind up with just the basename
		header.Name = path

		// zero out timestamps for reproducibility
		header.AccessTime = sourceDateEpoch
		header.ModTime = sourceDateEpoch
		header.ChangeTime = sourceDateEpoch

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			data, err := fsys.Open(path)
			if err != nil {
				return err
			}

			defer data.Close()

			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

// Writes a tarball to a temporary file.  Caller's responsibility to
// clean it up when it's done with it.
func WriteArchive(src string, w io.Writer, sourceDateEpoch time.Time) error {
	fs := os.DirFS(src)
	if err := WriteArchiveFromFS(src, fs, w, sourceDateEpoch); err != nil {
		return fmt.Errorf("writing TAR archive failed: %w", err)
	}

	return nil
}
