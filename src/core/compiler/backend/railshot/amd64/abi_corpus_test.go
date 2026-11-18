//go:build amd64

package amd64

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The oracle is an independently saved current-main2dd082d4 export, never a
// second compilation of the candidate. The baseline file hash pins that receipt.
func TestABITransferIndependentCorpus(t *testing.T) {
	root := os.Getenv("WAGO_RECOVERED_CORPUS")
	if root == "" {
		t.Skip("explicit recovered corpus directory required")
	}
	baselinePath := filepath.Join(root, "validated-hints-proof", "current-main.json")
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	bh := sha256.Sum256(baseline)
	if hex.EncodeToString(bh[:]) != "be684b6b069e9079c75208384fe48a9e8a365d4c15b78d910d01ed835462e844" {
		t.Fatal("independent main baseline receipt changed")
	}
	type row struct {
		ID               string
		Interruptible    bool
		Native, Metadata string
	}
	type key struct {
		id            string
		interruptible bool
	}
	var expected []row
	if err = json.Unmarshal(baseline, &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected) != 290 {
		t.Fatalf("baseline rows=%d", len(expected))
	}
	oracle := map[key]row{}
	for _, r := range expected {
		k := key{r.ID, r.Interruptible}
		if _, exists := oracle[k]; exists {
			t.Fatal("duplicate baseline row", k)
		}
		oracle[k] = r
	}
	raw, err := os.ReadFile(filepath.Join(root, "workloads145-recovered.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contracts []struct{ ID, SHA256 string }
	if err = json.Unmarshal(raw, &contracts); err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 145 {
		t.Fatalf("contracts=%d", len(contracts))
	}
	var rows []row
	seen := map[key]bool{}
	for _, c := range contracts {
		b, err := os.ReadFile(filepath.Join(root, "corpus-recovered", c.SHA256+".wasm"))
		if err != nil {
			t.Fatal(c.ID, err)
		}
		hash := sha256.Sum256(b)
		if hex.EncodeToString(hash[:]) != c.SHA256 {
			t.Fatal("input hash mismatch", c.ID)
		}
		for _, interruptible := range []bool{false, true} {
			k := key{c.ID, interruptible}
			want, ok := oracle[k]
			if !ok || seen[k] {
				t.Fatal("missing or duplicate contract key", k)
			}
			seen[k] = true
			m, err := wasm.DecodeModule(b)
			if err != nil {
				t.Fatal(c.ID, err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(c.ID, err)
			}
			opts := CompileOptions{Workers: 1, DeferCodeMapping: true, Interruptible: interruptible}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(c.ID, err)
			}
			if cm.CodeImage != nil {
				t.Fatal("unexpected mapped image", c.ID)
			}
			native := sha256.Sum256(cm.Code)
			code := cm.Code
			cm.Code = nil
			metadata, err := json.Marshal(cm)
			if err != nil {
				t.Fatal(c.ID, err)
			}
			mh := sha256.Sum256(metadata)
			got := row{c.ID, interruptible, hex.EncodeToString(native[:]), hex.EncodeToString(mh[:])}
			rows = append(rows, got)
			if got.Native != want.Native || got.Metadata != want.Metadata {
				t.Errorf("independent main mismatch %s interruptible=%v native=%s want=%s metadata=%s want=%s", c.ID, interruptible, got.Native, want.Native, got.Metadata, want.Metadata)
				if out := os.Getenv("WAGO_RECOVERED_OUTPUT"); out != "" {
					stem := out + "." + c.SHA256
					if interruptible {
						stem += ".interruptible"
					}
					if err = os.WriteFile(stem+".native.bin", code, 0644); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(stem+".metadata.json", metadata, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	if len(seen) != len(oracle) {
		t.Fatalf("compared %d of %d independent rows", len(seen), len(oracle))
	}
	if out := os.Getenv("WAGO_RECOVERED_OUTPUT"); out != "" {
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(out, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("compilations=%d independent native+metadata comparisons=%d baseline2dd082d4 receipt=%x", len(rows), len(seen), bh)
}
