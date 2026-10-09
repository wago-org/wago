//go:build linux && amd64 && wago_sumunroll

package amd64

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"os"
	"path/filepath"
	"testing"
)

// This records the sizing predictor separately from exact emitter admission.
// It runs outside all benchmark timing windows.
func TestSumUnrollHeadroomCorpus(t *testing.T) {
	dir := os.Getenv("WAGO_SUM_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set WAGO_SUM_ARTIFACT_DIR")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "..", "..", "..", "..", "corpus", "workloads")
	type record struct {
		Path, InputSHA256, DecodeError string
		QualifyingLocalFunctions       []int
		LogicalHeadroom, BaseCapacity  int
	}
	var records []record
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".wasm" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		r := record{Path: rel, InputSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
		m, err := frontend.DecodeValidate(data)
		if err != nil {
			r.DecodeError = err.Error()
			records = append(records, r)
			return nil
		}
		hints, _, _, err := computeModuleHintsWithWorkersResidencyPolicy(m, m.GlobalCount(), m.ImportedFuncCount(), 1, nil, false, currentCodegenPolicy(), false)
		if err != nil {
			return err
		}
		total := 0
		for i := range m.Code {
			total += len(m.Code[i].BodyBytes)
			if sumUnrollCodeHeadroom(m, hints, i, 0) > 0 {
				r.QualifyingLocalFunctions = append(r.QualifyingLocalFunctions, i)
			}
			r.LogicalHeadroom += sumUnrollCodeHeadroom(m, hints, i, r.LogicalHeadroom)
		}
		r.BaseCapacity = moduleCodeCapacityAMD64(total, len(m.Code), currentCodegenPolicy())
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "headroom-corpus.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
