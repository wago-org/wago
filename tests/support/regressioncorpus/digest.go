package regressioncorpus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func FileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func DirectArtifactsSHA256(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "module.*.wasm"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no module.*.wasm artifacts")
	}
	sort.Strings(matches)
	h := sha256.New()
	for _, file := range matches {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		h.Write([]byte(filepath.Base(file)))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
