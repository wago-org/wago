//go:build linux && amd64

package amd64

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Check complete compiled modules: a native leaf that is never selected does
// not satisfy this regression. Missing disassembly is a diagnostic failure.
func TestGCResolversLoadSpaceByte(t *testing.T) {
	objdump, err := exec.LookPath("objdump")
	if err != nil {
		t.Fatalf("space-load code regression requires objdump: %v", err)
	}
	spaceLoad := regexp.MustCompile(`movzx\s+([^,]+),BYTE PTR \[[^\]]+\+0xe\]`)
	for _, tc := range []struct {
		name                           string
		composite, param, result, body []byte
		shared                         bool
	}{
		{"inline-struct", []byte{0x5f, 1, 0x7f, 1}, []byte{0x63, 0}, []byte{0x7f}, []byte{0x20, 0, 0xfb, 2, 0, 0}, false},
		{"shared-open-struct", []byte{0x50, 0, 0x5f, 1, 0x7f, 1}, []byte{0x63, 0}, []byte{0x7f}, []byte{0x20, 0, 0xfb, 2, 0, 0}, true},
		{"defined-cast", []byte{0x5f, 1, 0x7f, 1}, []byte{0x6d}, []byte{0x7f}, []byte{0x20, 0, 0xfb, 22, 0, 0xd1}, false},
		{"defined-test", []byte{0x5f, 1, 0x7f, 1}, []byte{0x6d}, []byte{0x7f}, []byte{0x20, 0, 0xfb, 20, 0}, false},
		{"cast-array-length", []byte{0x5e, 0x6d, 1}, []byte{0x6d}, []byte{0x7f}, []byte{0x20, 0, 0xfb, 22, 0, 0xfb, 15}, false},
		{"array-reference-get", []byte{0x5e, 0x6d, 1}, []byte{0x63, 0}, []byte{0x6d}, []byte{0x20, 0, 0x41, 0, 0xfb, 11, 0}, false},
		{"cast-struct-get", []byte{0x5f, 1, 0x7f, 1}, []byte{0x6d}, []byte{0x7f}, []byte{0x20, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ft := append([]byte{0x60, 1}, tc.param...)
			ft = append(ft, 1)
			ft = append(ft, tc.result...)
			body := append(append([]byte(nil), tc.body...), 0x0b)
			m, err := wasm.DecodeModule(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(tc.composite, ft)),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
			))
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: optionalTestStats(&stats)})
			if err != nil {
				t.Fatal(err)
			}
			defer cm.CodeImage.Close()
			path := filepath.Join(t.TempDir(), "module.bin")
			if err := os.WriteFile(path, cm.Code, 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(objdump, "-D", "-b", "binary", "-m", "i386:x86-64", "-Mintel", path).CombinedOutput()
			if err != nil {
				t.Fatalf("objdump: %v\n%s", err, out)
			}
			matches := spaceLoad.FindAllStringSubmatch(string(out), -1)
			if len(matches) != 1 {
				t.Fatalf("native space byte loads = %d, want 1; old metadata shift/mask must be removed\n%s", len(matches), out)
			}
			if tc.name != "inline-struct" {
				// Keep both original space guards before the branchless exact
				// descriptor loads. No speculative Old descriptor read is needed.
				patterns := []string{
					`movzx\s+r10d,BYTE PTR \[r9(?:\+riz\*1)?\+0xe\]`,
					`test\s+r10d,r10d`,
					`cmp\s+r10d,0x4`,
					`shl\s+r10d,0x4`,
					`mov\s+r11,QWORD PTR \[r8\+r10\*1\+0x18\]`,
					`mov\s+r8d,DWORD PTR \[r8\+r10\*1\+0x20\]`,
				}
				for _, forbidden := range []string{`cmp\s+r10d,0x2`, `mov\s+r11,QWORD PTR \[r8\+0x38\]`, `mov\s+ecx,DWORD PTR \[r8\+0x40\]`} {
					if regexp.MustCompile(forbidden).Match(out) {
						t.Fatalf("Old specialization remains: %s\n%s", forbidden, out)
					}
				}
				remaining := string(out)
				for _, pattern := range patterns {
					at := regexp.MustCompile(pattern).FindStringIndex(remaining)
					if at == nil {
						t.Fatalf("indexed descriptor sequence missing %s\n%s", pattern, out)
					}
					remaining = remaining[at[1]:]
				}
			}
			reg := regexp.QuoteMeta(strings.TrimSpace(matches[0][1]))
			for _, old := range []string{`shr\s+` + reg + `,0x10`, `and\s+` + reg + `,0xff`} {
				if regexp.MustCompile(old + `\s`).Match(out) {
					t.Fatalf("old space decode remains: %s\n%s", old, out)
				}
			}
			if diagnosticsEnabled {
				calls := stats.Funcs[0].Calls
				wantCalls := uint64(1)
				if tc.name == "inline-struct" {
					wantCalls = 0
				}
				if got := calls["gcnative"] + calls["gcnative-leaf"]; uint64(got) != wantCalls || calls[callKindHostSync] != 0 {
					t.Fatalf("native calls = %v; want %d and no Go helper", calls, wantCalls)
				}
				if tc.shared && stats.GCSharedStubs != 1 {
					t.Fatalf("shared resolver bodies = %d, want 1", stats.GCSharedStubs)
				}
			}
		})
	}
}
