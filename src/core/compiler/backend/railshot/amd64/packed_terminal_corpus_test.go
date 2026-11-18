//go:build amd64

package amd64

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Compare complete native output and exported metadata over the frozen corpus.
// Full CompiledModule JSON includes every exported metadata field; deferred
// mapping makes CodeImage nil and removes allocation addresses from the result.
func TestPackedTerminalRecoveredCorpus(t *testing.T) {
	old := packedTerminalEnabled
	t.Cleanup(func() { packedTerminalEnabled = old })
	root := os.Getenv("WAGO_RECOVERED_CORPUS")
	if root == "" {
		t.Skip("explicit recovered corpus directory required")
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
	type row struct {
		ID               string
		Interruptible    bool
		Native, Metadata string
		Selected         int
	}
	var rows []row
	counts := map[string]int{}
	total := 0
	for _, c := range contracts {
		b, err := os.ReadFile(filepath.Join(root, "corpus-recovered", c.SHA256+".wasm"))
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != c.SHA256 {
			t.Fatalf("artifact hash %s", c.ID)
		}
		for _, interruptible := range []bool{false, true} {
			var baseline []byte
			var baselineCode []byte
			for _, enabled := range []bool{false, true} {
				packedTerminalEnabled = enabled
				m, err := wasm.DecodeModule(b)
				if err != nil {
					t.Fatal(c.ID, err)
				}
				if err = wasm.ValidateModule(m); err != nil {
					t.Fatal(c.ID, err)
				}
				var stats ModuleStats
				opts := CompileOptions{Workers: 1, DeferCodeMapping: true, Interruptible: interruptible}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				cm, err := CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(c.ID, err)
				}
				encoded, err := json.Marshal(cm)
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					t.Fatal("unexpected mapped image")
				}
				if !enabled {
					baseline = encoded
					baselineCode = append([]byte(nil), cm.Code...)
					continue
				}
				if !bytes.Equal(baseline, encoded) {
					_ = os.WriteFile(filepath.Join(root, "packed-mismatch-off.bin"), baselineCode, 0644)
					_ = os.WriteFile(filepath.Join(root, "packed-mismatch-on.bin"), cm.Code, 0644)
					t.Fatalf("native/metadata mismatch %s interruptible=%v oldbytes=%d newbytes=%d", c.ID, interruptible, len(baselineCode), len(cm.Code))
				}
				selected := 0
				for _, s := range stats.Funcs {
					selected += s.Peephole["packed-terminal"]
					for k, v := range s.Peephole {
						if len(k) >= 6 && k[:6] == "packed" {
							counts[k] += v
						}
					}
				}
				total += selected
				native := sha256.Sum256(cm.Code)
				cm.Code = nil
				meta, err := json.Marshal(cm)
				if err != nil {
					t.Fatal(err)
				}
				mh := sha256.Sum256(meta)
				rows = append(rows, row{c.ID, interruptible, hex.EncodeToString(native[:]), hex.EncodeToString(mh[:]), selected})
			}
		}
	}
	if diagnosticsEnabled && !profileEnabled && !regallocCheckEnabled && total == 0 {
		t.Fatal("candidate never selected")
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
	t.Logf("counts=%v", counts)
	t.Logf("rows=%d off/on compilations=%d selected=%d diagnostics=%v", len(rows), len(rows)*2, total, diagnosticsEnabled)
}
