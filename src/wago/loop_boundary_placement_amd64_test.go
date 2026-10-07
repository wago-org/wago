//go:build amd64 && (linux || darwin || windows) && !tinygo

package wago

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type loopBoundaryPlacement struct {
	header, backedge uint64
	multiplies       []uint64
}

// Inspect only these bounded, single-backedge fixtures. A mnemonic count alone
// cannot distinguish a producer outside the loop from one repeated inside it.
// Parse instruction addresses and direct branch operands so displacement and
// immediate bytes are not mistaken for multiply or branch opcodes.
func inspectLoopBoundary(disassembly string) (loopBoundaryPlacement, error) {
	var result loopBoundaryPlacement
	edges := 0
	for _, line := range strings.Split(disassembly, "\n") {
		address, rest, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		pc, err := strconv.ParseUint(address, 16, 64)
		if err != nil {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		op := fields[0]
		if op == "mulsd" || op == "vmulsd" {
			result.multiplies = append(result.multiplies, pc)
		}
		if strings.HasPrefix(op, "j") && len(fields) > 1 && strings.HasPrefix(fields[1], "0x") {
			target, err := strconv.ParseUint(fields[1][2:], 16, 64)
			if err != nil {
				return result, err
			}
			if target < pc {
				result.header, result.backedge = target, pc
				edges++
			}
		}
	}
	if edges != 1 {
		return result, fmt.Errorf("backedges=%d, want one", edges)
	}
	return result, nil
}

func (p loopBoundaryPlacement) producerInside() bool {
	for _, pc := range p.multiplies {
		if p.header <= pc && pc <= p.backedge {
			return true
		}
	}
	return false
}

func TestLoopBoundaryNativePlacementAMD64(t *testing.T) {
	var objdump string
	for _, name := range []string{"objdump", "gobjdump"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		version, err := exec.Command(path, "--version").CombinedOutput()
		if err == nil && strings.Contains(string(version), "GNU objdump") {
			objdump = path
			break
		}
	}
	if objdump == "" {
		t.Skip("GNU objdump is required for instruction validation")
	}
	for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
		for _, fixture := range loopBoundaryFixtures {
			t.Run(fmt.Sprintf("features=%x/%s", features, fixture.Name), func(t *testing.T) {
				raw := watToWasm(t, fixture.WAT())
				m, err := wasm.DecodeModule(raw)
				if err != nil {
					t.Fatal(err)
				}
				if err = wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				var stats backend.ModuleStats
				opts := backend.CompileOptions{AMD64FeaturesSet: true, AMD64Features: features}
				if compilerTelemetryEnabled {
					opts.Stats = &stats
				}
				c, err := backend.CompileModuleWith(m, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer c.CodeImage.Close()
				// These fixtures exercise the established compiler, including real spills.
				// Counters are populated only with wago_codegenstats (or profiling) enabled.
				if len(stats.Funcs) > 0 {
					s := stats.Funcs[0]
					if s.SharedScalar {
						t.Fatal("fixture unexpectedly used the shared compiler")
					}
					if fixture.RegisterPressure && s.Spills == 0 {
						t.Fatal("register-pressure fixture did not exercise allocator spills")
					}
				}
				path := filepath.Join(t.TempDir(), "code.bin")
				if err = os.WriteFile(path, c.Code[c.Entry[0]:], 0600); err != nil {
					t.Fatal(err)
				}
				output, err := exec.Command(objdump, "-D", "-b", "binary", "-m", "i386:x86-64", "-M", "intel", "--no-show-raw-insn", path).CombinedOutput()
				if err != nil {
					t.Fatalf("objdump: %v\n%s", err, output)
				}
				placement, err := inspectLoopBoundary(string(output))
				if err != nil {
					t.Fatalf("%v\n%s", err, output)
				}
				// Constant folding may legitimately remove the constant producer entirely.
				if len(placement.multiplies) != 1 && !(fixture.Constant && len(placement.multiplies) == 0) {
					t.Fatalf("multiply sites=%v; want one producer\n%s", placement.multiplies, output)
				}
				if got := placement.producerInside(); got != fixture.Inside {
					t.Fatalf("producer inside loop=%v, want %v: %+v\n%s", got, fixture.Inside, placement, output)
				}
				if !fixture.Inside {
					for _, pc := range placement.multiplies {
						if pc >= placement.header {
							t.Fatalf("producer %#x must precede header %#x", pc, placement.header)
						}
					}
				}
			})
		}
	}
}

func TestLoopBoundaryPlacementObserverAMD64(t *testing.T) {
	// Same mnemonic counts, different branch-target placement. The observer must
	// reject the repeated-work control as a pre-loop producer before execution.
	before := `10: vmulsd xmm0,xmm14,xmm15
20: vaddsd xmm13,xmm13,xmm0
30: jmp 0x20`
	inside := `20: vmulsd xmm0,xmm14,xmm15
25: vaddsd xmm13,xmm13,xmm0
30: jmp 0x20`
	for _, tc := range []struct {
		code   string
		inside bool
	}{{before, false}, {inside, true}} {
		p, err := inspectLoopBoundary(tc.code)
		if err != nil || len(p.multiplies) != 1 || p.producerInside() != tc.inside {
			t.Fatalf("placement=%+v err=%v want inside=%v", p, err, tc.inside)
		}
	}
	for _, code := range []string{"10: vmulsd xmm0,xmm1,xmm2", before + "\n40: jmp 0x10"} {
		if _, err := inspectLoopBoundary(code); err == nil {
			t.Fatal("ambiguous control flow accepted")
		}
	}
}
