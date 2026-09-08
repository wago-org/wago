package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectMetadataLimits(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{Path(dir), LockPath(dir), projectJournalPath(dir)} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		size := int64(maxProjectMetadataBytes + 1)
		if path == projectJournalPath(dir) {
			size = maxProjectJournalBytes + 1
		}
		if err := file.Truncate(size); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := readManifest(dir); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("manifest: %v", err)
	}
	if _, err := readLock(dir); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("lock: %v", err)
	}
	if err := (&Mutation{dir: dir}).recover(); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("journal: %v", err)
	}
	for _, data := range []string{
		`{"custom":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}",
		`{"custom":[` + strings.Repeat("0,", 100000) + "0]}",
	} {
		if _, err := decodeManifest([]byte(data), dir); err == nil || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("structure limit: %v", err)
		}
	}
}

func TestEncodedProjectMetadataIncludesNewlineInLimit(t *testing.T) {
	for _, kind := range []string{"manifest", "lock"} {
		t.Run(kind, func(t *testing.T) {
			plugins := map[string]any{"github.com/acme/plugin": "^1.0.0"}
			manifest := map[string]any{"plugins": plugins}
			lock := NewLockDocument()
			entry := testLockEntry(true, "github.com/acme/plugin", map[string]string{})
			entry.Config = json.RawMessage(`{"custom":""}`)
			lock.Plugins["github.com/acme/plugin"] = entry
			var document any = manifest
			if kind == "lock" {
				document = lock
			}
			empty, err := json.MarshalIndent(document, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			for _, extra := range []int{0, 1} {
				padding := strings.Repeat(" ", maxProjectMetadataBytes-len(empty)-1+extra)
				plugins["github.com/acme/plugin"] = "^1.0.0" + padding
				entry.Config = json.RawMessage(`{"custom":"` + padding + `"}`)
				lock.Plugins["github.com/acme/plugin"] = entry
				var data []byte
				if kind == "manifest" {
					data, err = EncodeManifest(manifest)
				} else {
					data, err = EncodeLock(lock)
				}
				if extra != 0 {
					if err == nil || !strings.Contains(err.Error(), "byte limit") {
						t.Fatalf("one byte over limit: length %d, error %v", len(data), err)
					}
					continue
				}
				if err != nil || len(data) != maxProjectMetadataBytes || data[len(data)-1] != '\n' {
					t.Fatalf("exact limit: length %d, error %v", len(data), err)
				}
				if kind == "manifest" {
					_, err = decodeManifest(data, t.TempDir())
				} else {
					_, err = DecodeLock(data)
				}
				if err != nil {
					t.Fatalf("decode encoder output: %v", err)
				}
			}
		})
	}
}
