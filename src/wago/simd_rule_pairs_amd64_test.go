//go:build (linux || darwin) && amd64 && !tinygo && !wago_precompiled

package wago

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func simdPairProfiles() []uint64 {
	out := []uint64{0}
	host, _ := cachedAMD64CPUFeatures()
	modern := shared.AMD64ModernBaseline | shared.AMD64AVX2
	if host.Has(modern) {
		out = append(out, uint64(modern))
	}
	return out
}

func compileSIMDPair(t testing.TB, raw []byte, profile uint64) simdPairCode {
	t.Helper()
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	var stats railshotModuleStats
	opts := railshotCompileOptions{Workers: 1, DeferCodeMapping: true, AMD64FeaturesSet: true, AMD64Features: shared.AMD64Features(profile)}
	if compilerTelemetryEnabled {
		opts.Stats = &stats
	}
	cm, err := railshotCompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		t.Fatal("unexpected mapped code image")
	}
	if cm.RequiredAMD64Features&^uint32(profile) != 0 {
		t.Fatal("emitted features exceed profile")
	}
	out := simdPairCode{bytes: cm.Code, entry: uint32(cm.Entry[0]), spills: -1, literals: -1}
	if compilerTelemetryEnabled {
		if len(stats.Funcs) != 1 || stats.Funcs[0].SharedScalar {
			t.Fatal("expected established SIMD compiler")
		}
		out.spills = stats.Funcs[0].Spills
		out.literals = stats.Funcs[0].NativeSize.LiteralPoolBytes
	}
	return out
}

func TestSIMDSourcePairSelectionAMD64(t *testing.T) {
	if !compilerTelemetryEnabled {
		t.Skip("selection inspection requires wago_codegenstats")
	}
	dump, err := exec.LookPath("objdump")
	if err != nil {
		t.Skip("GNU objdump is required")
	}
	version, err := exec.Command(dump, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(version), "GNU objdump") {
		t.Skip("GNU objdump is required")
	}
	for _, family := range []string{"average", "high-byte", "dot-sub", "dot-shifts"} {
		for _, profile := range simdPairProfiles() {
			t.Run(fmt.Sprintf("%s/profile=%x", family, profile), func(t *testing.T) {
				for _, simple := range []bool{false, true} {
					raw := simdPairModule(t, family, simple)
					c := compileSIMDPair(t, raw, profile)
					path := filepath.Join(t.TempDir(), "native.bin")
					if err := os.WriteFile(path, c.bytes, 0600); err != nil {
						t.Fatal(err)
					}
					// The one-function fixture puts its literals after the instruction body.
					end := len(c.bytes) - c.literals
					out, err := exec.Command(dump, "-D", "-b", "binary", "-m", "i386:x86-64", "-M", "intel", "--no-show-raw-insn", "--start-address="+strconv.Itoa(int(c.entry)), "--stop-address="+strconv.Itoa(end), path).CombinedOutput()
					if err != nil {
						t.Fatalf("objdump: %v %s", err, out)
					}
					count := 0
					for _, line := range strings.Split(string(out), "\n") {
						fields := strings.Fields(line)
						if len(fields) < 2 || !strings.HasSuffix(fields[0], ":") {
							continue
						}
						op := strings.TrimPrefix(fields[1], "v")
						if family == "average" && op == "pavgb" || family == "dot-sub" && op == "pmaddwd" || family == "high-byte" && op == "psrld" && strings.Contains(line, ",0x1e") || family == "dot-shifts" && op == "psrad" && strings.Contains(line, ",0x10") {
							count++
						}
					}
					t.Logf("simple=%v selected-ops=%d native-B=%d literal-B=%d spills=%d", simple, count, len(c.bytes), c.literals, c.spills)
					// Replacing the simplified artifact with the valid original is a
					// disabled-substitution control. It must fail this same selection gate.
					want := 1
					if family == "dot-shifts" {
						want = 2
					}
					if (count == want) != simple {
						t.Fatalf("selected instruction count=%d simple=%v\n%s", count, simple, out)
					}
				}
			})
		}
	}
}
