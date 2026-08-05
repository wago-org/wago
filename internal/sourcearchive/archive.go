// Package sourcearchive safely extracts source archives with one top-level directory.
package sourcearchive

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extract expands archive into target while removing the archive's single
// top-level directory. It rejects traversal, multiple roots, and oversized
// expanded content.
func Extract(archive, target string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()

	root := ""
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		part := strings.SplitN(name, "/", 2)[0]
		if part == "" || part == "." || part == ".." {
			return errors.New("source archive contains an invalid root")
		}
		if root == "" {
			root = part
		} else if root != part {
			return errors.New("source archive contains multiple roots")
		}
	}
	if root == "" {
		return errors.New("source archive is empty")
	}

	const maxExpandedSize = 512 << 20
	var expanded uint64
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		relative := strings.TrimPrefix(name, root+"/")
		if relative == "" {
			continue
		}
		destination := filepath.Join(target, filepath.FromSlash(relative))
		if !strings.HasPrefix(filepath.Clean(destination), filepath.Clean(target)+string(os.PathSeparator)) {
			return errors.New("source archive path escapes destination")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return err
			}
			continue
		}
		expanded += entry.UncompressedSize64
		if expanded > maxExpandedSize {
			return errors.New("source archive expands beyond 512 MiB limit")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, entry.Mode().Perm())
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(destinationFile, source)
		closeErr := destinationFile.Close()
		sourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if sourceErr != nil {
			return sourceErr
		}
	}
	if _, err := os.Stat(filepath.Join(target, "go.mod")); err != nil {
		return errors.New("source archive does not contain go.mod")
	}
	return nil
}
