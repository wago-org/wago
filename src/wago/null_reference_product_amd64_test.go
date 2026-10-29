//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestStagedFirstNullReferenceProductRejectsWidening(t *testing.T) {
	mutable, err := compileStagedNullReferenceProductForTest(stagedFirstNullReferenceModule(true))
	if err != nil {
		t.Fatalf("mutable anyref global compile = %v", err)
	}
	_ = mutable.Close()

	m, err := wasm.DecodeModule(stagedFirstNullReferenceModule(false))
	if err != nil {
		t.Fatal(err)
	}
	features := frontend.AllFeatures()
	features.TypedFunctionReferences = true
	features.NullReferenceProducts = false
	if err := frontend.RejectUnsupportedWithFeatures(m, features); err == nil || !strings.Contains(err.Error(), "ref null any") {
		t.Fatalf("frontend without null-product gate = %v, want explicit any-reference rejection", err)
	}
}

func TestStagedBottomNullReferenceGlobalsRejectWidening(t *testing.T) {
	mutable, err := compileStagedNullReferenceProductForTest(stagedBottomNullReferenceModule(true))
	if err != nil {
		t.Fatalf("mutable bottom global compile = %v", err)
	}
	_ = mutable.Close()
}

func BenchmarkStagedBottomNullReferenceGlobalGet(b *testing.B) {
	c, err := compileStagedNullReferenceProductForTest(stagedBottomNullReferenceModule(false))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("nullexnref")
		if err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("Invoke = %v, %v", got, err)
		}
	}
}

func replayStagedNullReferenceScript(t *testing.T, base, tmp string, script stagedSpecScript) (counts stagedSpecCounts, gates map[string]int) {
	t.Helper()
	gates = map[string]int{}
	definitions := map[string][]byte{}
	var latestDefinition []byte
	var current stagedSpecModule
	named := map[string]stagedSpecModule{}
	var live []stagedSpecModule
	defer func() {
		for i := len(live) - 1; i >= 0; i-- {
			_ = live[i].in.Close()
			_ = live[i].c.Close()
		}
	}()
	instantiate := func(data []byte, cmd stagedSpecCommand) (stagedSpecModule, error) {
		decoded, err := wasm.DecodeModule(data)
		if err != nil {
			return stagedSpecModule{}, fmt.Errorf("decode: %w", err)
		}
		kind, err := stagedNullReferenceProductShape(decoded)
		if err != nil {
			return stagedSpecModule{}, fmt.Errorf("shape: %w", err)
		}
		wantBytes := 189
		if kind == stagedNullReferenceProductBottomGlobals {
			wantBytes = 364
		}
		if len(data) != wantBytes {
			return stagedSpecModule{}, fmt.Errorf("pinned module size %d, want %d", len(data), wantBytes)
		}
		c, err := compileStagedNullReferenceProductForTest(data)
		if err != nil {
			return stagedSpecModule{}, fmt.Errorf("compile: %w", err)
		}
		in, err := instantiateCore(c, InstantiateOptions{})
		if err != nil {
			_ = c.Close()
			return stagedSpecModule{}, fmt.Errorf("instantiate: %w", err)
		}
		if in.gc != nil {
			_ = in.Close()
			_ = c.Close()
			return stagedSpecModule{}, fmt.Errorf("null-only product allocated a collector")
		}
		m := stagedSpecModule{in: in, c: c}
		live = append(live, m)
		if cmd.Name != "" {
			named[cmd.Name] = m
		}
		return m, nil
	}
	for _, cmd := range script.Commands {
		counts.Commands++
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module definition: %v", base, cmd.Line, err)
				continue
			}
			latestDefinition = data
			if cmd.Name != "" {
				definitions[cmd.Name] = data
			}
		case "module_instance", "module":
			var data []byte
			var err error
			if cmd.Type == "module" {
				data, err = os.ReadFile(filepath.Join(tmp, cmd.Filename))
			} else if cmd.Module != "" {
				data = definitions[cmd.Module]
			} else {
				data = latestDefinition
			}
			if err != nil || data == nil {
				counts.Failures++
				t.Errorf("%s.wast:%d read module: %v", base, cmd.Line, err)
				continue
			}
			current, err = instantiate(data, cmd)
			if err != nil {
				if strings.Contains(err.Error(), "compile:") {
					counts.UnexpectedCompileRejects++
				} else {
					counts.UnexpectedLinkRejects++
				}
				counts.Failures++
				t.Errorf("%s.wast:%d module rejected: %v", base, cmd.Line, err)
				continue
			}
			counts.ModulesPassed++
		case "assert_return":
			m := current
			if cmd.Action.Module != "" {
				m = named[cmd.Action.Module]
			}
			if m.in == nil || cmd.Action.Type != "invoke" || len(cmd.Action.Args) != 0 {
				counts.Failures++
				t.Errorf("%s.wast:%d unavailable or unsupported null action", base, cmd.Line)
				continue
			}
			got, err := m.in.Invoke(cmd.Action.Field)
			if err != nil || len(got) != len(cmd.Expected) {
				counts.Failures++
				t.Errorf("%s.wast:%d result=%v err=%v want=%v", base, cmd.Line, got, err, cmd.Expected)
				continue
			}
			matched := true
			for i := range got {
				if !stagedTypedReferenceMatch(m, got[i], cmd.Expected[i]) {
					matched = false
					break
				}
			}
			if !matched {
				counts.Failures++
				t.Errorf("%s.wast:%d result=%v want=%v", base, cmd.Line, got, cmd.Expected)
				continue
			}
			counts.AssertionsPassed++
		default:
			counts.Failures++
			t.Errorf("%s.wast:%d unhandled command %q", base, cmd.Line, cmd.Type)
		}
	}
	if current.in != nil {
		allocs := testing.AllocsPerRun(1000, func() {
			got, err := current.in.Invoke("nullexnref")
			if err != nil || len(got) != 1 || got[0] != 0 {
				panic("official bottom null-reference replay failed")
			}
		})
		if allocs != 0 {
			counts.Failures++
			t.Errorf("%s official null-reference allocations = %v, want 0", base, allocs)
		}
	}
	return counts, gates
}
