package railshot

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/spectest"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/arm32"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv32"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type pico2SpecScript struct {
	Commands []pico2SpecCommand `json:"commands"`
}

type pico2SpecCommand struct {
	Type     string `json:"type"`
	Filename string `json:"filename"`
	Line     int    `json:"line"`
}

// TestPico2Release2CompileAdmission converts the official Release 2 scripts and
// requires both embedded targets to make the same strict module-admission
// decision. It is opt-in because the WebAssembly/spec checkout and wabt are
// external qualification inputs:
//
//	WAGO_PICO2_SPECTEST_DIR=/path/to/WebAssembly/spec go test \
//	  ./src/core/compiler/backend/railshot -run Pico2Release2 -count=1
func TestPico2Release2CompileAdmission(t *testing.T) {
	checkout := os.Getenv("WAGO_PICO2_SPECTEST_DIR")
	if checkout == "" {
		t.Skip("WAGO_PICO2_SPECTEST_DIR is not set")
	}
	wast2json, err := exec.LookPath("wast2json")
	if err != nil {
		t.Skip("wast2json (wabt) not on PATH")
	}
	suite, err := spectest.DiscoverRelease2(checkout)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	modules, excluded := 0, 0
	for _, base := range suite.Files {
		base := base
		t.Run(strings.ReplaceAll(base, string(filepath.Separator), "_"), func(t *testing.T) {
			wast := filepath.Join(suite.CoreDir, base+".wast")
			name := strings.ReplaceAll(base, string(filepath.Separator), "_")
			jsonPath := filepath.Join(tmp, name+".json")
			if output, err := exec.Command(wast2json, wast, "-o", jsonPath).CombinedOutput(); err != nil {
				t.Fatalf("wast2json: %v: %s", err, output)
			}
			raw, err := os.ReadFile(jsonPath)
			if err != nil {
				t.Fatal(err)
			}
			var script pico2SpecScript
			if err := json.Unmarshal(raw, &script); err != nil {
				t.Fatal(err)
			}
			for commandIndex := range script.Commands {
				command := &script.Commands[commandIndex]
				if command.Filename == "" {
					continue
				}
				moduleBytes, err := os.ReadFile(filepath.Join(tmp, command.Filename))
				if err != nil {
					t.Fatalf("line %d %s: %v", command.Line, command.Type, err)
				}
				module, decodeErr := wasm.DecodeModule(moduleBytes)
				expectReject := command.Type == "assert_malformed" || command.Type == "assert_invalid"
				if decodeErr != nil {
					if !expectReject {
						t.Errorf("line %d %s: valid module decode failed: %v", command.Line, command.Type, decodeErr)
					}
					continue
				}
				if pico2OutsideCompletionGate(module) {
					excluded++
					continue
				}
				modules++
				_, armErr := arm32.CompileModule(module)
				_, rvErr := riscv32.CompileModule(module)
				if (armErr == nil) != (rvErr == nil) {
					t.Errorf("line %d %s: target admission differs: arm=%v rv=%v", command.Line, command.Type, armErr, rvErr)
					continue
				}
				if expectReject {
					if armErr == nil {
						t.Errorf("line %d %s: invalid module admitted", command.Line, command.Type)
					}
					continue
				}
				if armErr != nil {
					t.Errorf("line %d %s: valid module rejected: %v", command.Line, command.Type, armErr)
				}
			}
		})
	}
	t.Logf("Pico 2 Release 2 compile admission: modules=%d outside-gate=%d files=%d", modules, excluded, len(suite.Files))
}

func pico2OutsideCompletionGate(module *wasm.Module) bool {
	if module == nil || module.MemCount() > 1 || len(module.Tags) != 0 || len(module.StringRefs) != 0 {
		return true
	}
	for i := range module.Imports {
		in := &module.Imports[i]
		if in.Type.Kind == wasm.ExternTag || in.Type.Kind == wasm.ExternMem && (in.Type.Mem.Shared || in.Type.Mem.Limits.Addr64) || in.Type.Kind == wasm.ExternTable && in.Type.Table.Limits.Addr64 {
			return true
		}
	}
	for i := range module.Memories {
		if module.Memories[i].Shared || module.Memories[i].Limits.Addr64 {
			return true
		}
	}
	for i := range module.Tables {
		if module.Tables[i].Type.Limits.Addr64 {
			return true
		}
	}
	for i := range module.Types {
		for j := range module.Types[i].SubTypes {
			if module.Types[i].SubTypes[j].Comp.Kind != wasm.CompFunc {
				return true
			}
		}
	}
	return false
}

func TestPico2OutsideCompletionGate(t *testing.T) {
	if pico2OutsideCompletionGate(&wasm.Module{}) {
		t.Fatal("empty core module excluded")
	}
	cases := []*wasm.Module{
		{Memories: []wasm.MemType{{}, {}}},
		{Memories: []wasm.MemType{{Shared: true}}},
		{Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}},
		{Tags: []wasm.TagType{{}}},
		{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompStruct}}}}}},
	}
	for i, module := range cases {
		if !pico2OutsideCompletionGate(module) {
			t.Fatalf("case %d not excluded: %s", i, fmt.Sprint(module))
		}
	}
}
