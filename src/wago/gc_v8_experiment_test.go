//go:build linux && amd64

package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// This audit is separate from timing: it proves the admitted allocation cases
// actually collect, rather than silently measuring disabled collection.
func TestGCV8ExperimentCollection(t *testing.T) {
	const root = "../../bench/experiments/gc-v8"
	data, err := os.ReadFile(filepath.Join(root, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string `json:"name"`
		File   string `json:"file"`
		Export string `json:"export"`
		N      uint64 `json:"n"`
		Want   uint64 `json:"want"`
		SHA256 string `json:"sha256"`
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, tc.File))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			if hex.EncodeToString(digest[:]) != tc.SHA256 {
				t.Fatal("artifact digest mismatch")
			}
			switch tc.Name {
			case "struct-allocation-access", "array-allocation-access", "array-fill", "array-copy-checksum", "array-loop-copy-checksum", "tree-survival":
			default:
				return
			}
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false), data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for j := 0; j < 100; j++ {
				r, e := in.Invoke(tc.Export, tc.N)
				if e != nil || len(r) != 1 || r[0] != tc.Want {
					t.Fatalf("call %d: %v %v want %d", j, r, e, tc.Want)
				}
			}
			if in.gc == nil {
				t.Fatal("no collector")
			}
			s := in.gc.Stats()
			t.Logf("stats %+v", s)
			if s.MinorCollections+s.FullCollections == 0 {
				t.Fatal("allocation case did not collect")
			}
		})
	}
}
