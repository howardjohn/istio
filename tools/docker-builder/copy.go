package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"syscall"

	"istio.io/pkg/log"
)

func CopyDirectory(scrDir, dest string) error {
	log.Debugf("CopyDir %v -> %v", scrDir, dest)
	if err := os.MkdirAll(dest, 0o755); err != nil && os.IsExist(err) {
		return fmt.Errorf("mkdir: %v", err)
	}
	entries, err := ioutil.ReadDir(scrDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		sourcePath := filepath.Join(scrDir, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		fileInfo, err := os.Stat(sourcePath)
		if err != nil {
			return fmt.Errorf("stat: %v", err)
		}

		stat, ok := fileInfo.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get raw syscall.Stat_t data for '%s'", sourcePath)
		}

		switch fileInfo.Mode() & os.ModeType {
		case os.ModeDir:
			if err := CreateIfNotExists(destPath, 0o755); err != nil {
				return err
			}
			if err := CopyDirectory(sourcePath, destPath); err != nil {
				return err
			}
		case os.ModeSymlink:
			if err := CopySymLink(sourcePath, destPath); err != nil {
				return err
			}
		default:
			if err := Copy(sourcePath, destPath); err != nil {
				return fmt.Errorf("copy: %v", err)
			}
		}

		if err := os.Lchown(destPath, int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}

		isSymlink := entry.Mode()&os.ModeSymlink != 0
		if !isSymlink {
			if err := os.Chmod(destPath, entry.Mode()); err != nil {
				return err
			}
		}
	}
	return nil
}

func CopyGeneric(srcFile, dstFile string) error {
	if err := os.MkdirAll(path.Dir(dstFile), 0o750); err != nil {
		return fmt.Errorf("failed to make destination directory %v: %v", dstFile, err)
	}

	fi, err := os.Stat(srcFile)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return CopyDirectory(srcFile, dstFile)
	}
	return Copy(srcFile, dstFile)
}

func Copy(srcFile, dstFile string) error {
	log.Debugf("Copy %v -> %v", srcFile, dstFile)
	in, err := os.Open(srcFile)
	defer in.Close()
	if err != nil {
		return err
	}

	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}

	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return nil
}

func Exists(filePath string) bool {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return false
	}

	return true
}

func CreateIfNotExists(dir string, perm os.FileMode) error {
	if Exists(dir) {
		return nil
	}

	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("failed to create directory: '%s', error: '%s'", dir, err.Error())
	}

	return nil
}

func CopySymLink(source, dest string) error {
	link, err := os.Readlink(source)
	if err != nil {
		return err
	}
	return os.Symlink(link, dest)
}
