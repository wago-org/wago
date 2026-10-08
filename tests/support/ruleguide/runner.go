package ruleguide

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"

	cr "github.com/wago-org/wago/src/core/runtime"
)

// Artifact describes the exact code passed to MapCode. Selection is -1 when
// diagnostics are absent and -2 when the rule does not exist on this target.
type Artifact struct {
	Code                             []byte
	Entry                            int
	Selection                        int
	Shared                           []bool
	Required                         uint32
	SelectedFeatures                 uint32
	FrameBytes, Spills, LiteralBytes int
	Calls, InlineCalls               int
}
type Compiler func([]byte, bool, string) (Artifact, error)

func SelectionGate(a Artifact, selected bool) error {
	want := 0
	if selected {
		want = 1
	}
	if a.Selection < 0 {
		return fmt.Errorf("selection evidence unavailable (%d)", a.Selection)
	}
	if a.Selection != want {
		return fmt.Errorf("selected %d times, want %d", a.Selection, want)
	}
	return nil
}

// executor owns bounded ABI and trap storage, outside timed native calls.
type executor struct {
	eng                 *cr.Engine
	jm                  *cr.JobMemory
	ar                  *cr.Arena
	code                []byte
	entry               uintptr
	args, results, trap []byte
}

func prepare(a Artifact) (*executor, error) {
	e := new(executor)
	var err error
	e.eng, err = cr.NewEngine()
	if err != nil {
		return nil, err
	}
	e.jm, err = cr.NewJobMemory(65536)
	if err != nil {
		e.close()
		return nil, err
	}
	e.ar, err = cr.NewArena(4096)
	if err != nil {
		e.close()
		return nil, err
	}
	e.code, e.entry, err = cr.MapCode(a.Code)
	if err != nil {
		e.close()
		return nil, err
	}
	if !bytes.Equal(e.code[:len(a.Code)], a.Code) {
		e.close()
		return nil, fmt.Errorf("mapped native bytes differ")
	}
	e.entry += uintptr(a.Entry)
	e.args = e.ar.Alloc(32)
	e.results = e.ar.Alloc(32)
	e.trap = e.ar.Alloc(cr.TrapBufferBytes)
	return e, nil
}
func (e *executor) close() {
	if e.code != nil {
		cr.Unmap(e.code)
	}
	if e.ar != nil {
		e.ar.Close()
	}
	if e.jm != nil {
		e.jm.Close()
	}
	if e.eng != nil {
		e.eng.Close()
	}
}
func (e *executor) input(in [3]uint64) {
	clear(e.args)
	clear(e.results)
	for i, v := range in {
		binary.LittleEndian.PutUint64(e.args[i*8:], v)
	}
}
func (e *executor) call() error {
	return e.eng.Call(e.entry, e.args, e.jm.LinearMemory(), e.trap, e.results)
}
func (e *executor) check(r Recipe, in [3]uint64) error {
	want := r.Model(in)
	got := append([]byte(nil), e.results[:4]...)
	offset := 8
	if r.Family == "simd" {
		got = append(got[:0], e.results[:16]...)
		offset = 16
	}
	got = append(got, e.results[offset:offset+4]...)
	if !bytes.Equal(got, want) {
		return fmt.Errorf("input=%x result=%x want=%x", in, got, want)
	}
	return nil
}
func execute(a Artifact, r Recipe, inputs [][3]uint64) error {
	e, err := prepare(a)
	if err != nil {
		return err
	}
	defer e.close()
	for _, in := range inputs {
		e.input(in)
		if err = e.call(); err != nil {
			return err
		}
		if err = e.check(r, in); err != nil {
			return err
		}
	}
	return nil
}
func InputHash(inputs [][3]uint64) [32]byte {
	var b []byte
	for _, in := range inputs {
		for _, v := range in {
			b = binary.LittleEndian.AppendUint64(b, v)
		}
	}
	return sha256.Sum256(b)
}

func Run(t *testing.T, mode string, compile Compiler) {
	inputs := Inputs()
	recipes := Cases()
	controlCount := len(recipes)
	recipes = append(recipes, Generate(Seed, false)...)
	recipes = append(recipes, Generate(Seed, true)...)
	for index, r := range recipes {
		t.Run(r.Name(), func(t *testing.T) {
			raw := r.Wasm()
			a, err := compile(raw, false, r.Rule())
			if err != nil {
				t.Fatal(err)
			}
			if a.Selection >= 0 {
				if err = SelectionGate(a, r.Selected()); err != nil {
					t.Fatal(err)
				}
			}
			if len(a.Shared) > 0 {
				if len(a.Shared) != 2 || a.Shared[0] {
					t.Fatalf("main path is not established: %v", a.Shared)
				}
				calls := 0
				if r.Context == "live-call" {
					calls = 1
				}
				if a.Calls != calls || a.InlineCalls != 0 {
					t.Fatalf("actual calls=%d inlined=%d, want %d non-inlined calls", a.Calls, a.InlineCalls, calls)
				}
			}
			if err = execute(a, r, inputs); err != nil {
				t.Fatal(err)
			}
			t.Logf("seed=%d go=%s target=%s/%s mode=%s bounds=explicit api=Engine.Call source=%x native=%x inputs=%x nativeB=%d selected-features=%x required=%x selection=%d shared=%v frameB=%d spills=%d literalB=%d calls=%d inline=%d completed=%d", Seed, runtime.Version(), runtime.GOOS, runtime.GOARCH, mode, sha256.Sum256(raw), sha256.Sum256(a.Code), InputHash(inputs), len(a.Code), a.SelectedFeatures, a.Required, a.Selection, a.Shared, a.FrameBytes, a.Spills, a.LiteralBytes, a.Calls, a.InlineCalls, len(inputs))
			if index >= controlCount || !r.Selected() || a.Selection < 0 {
				return
			}
			// Use only valid source and native code in the observation controls.
			var control Artifact
			controlRaw := raw
			controlInputs := inputs
			if r.Family == "simd" {
				substitute := r
				substitute.Variable = true
				controlRaw = substitute.Wasm()
				control, err = compile(controlRaw, false, r.Rule())
				controlInputs = append([][3]uint64(nil), inputs...)
				for i := range controlInputs {
					controlInputs[i][2] = uint64(uint32(r.Count))
				}
				if err == nil {
					err = execute(control, r, controlInputs)
				}
			} else {
				control, err = compile(raw, true, r.Rule())
				if err == nil {
					err = execute(control, r, inputs)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if control.Selection != 0 {
				t.Fatalf("valid inactive control still selected rule: %d", control.Selection)
			}
			if SelectionGate(control, true) == nil {
				t.Fatal("inactive control accepted as selected")
			}
			t.Logf("valid-inactive-control source=%x native=%x inputs=%x selection=%d completed=%d", sha256.Sum256(controlRaw), sha256.Sum256(control.Code), InputHash(controlInputs), control.Selection, len(controlInputs))
		})
	}
}

// Yield measures generation, validation, compilation and two independently
// checked native calls per candidate. Useful means the requested rule actually
// activated and the calls matched the model. Useful cases are deduplicated by
// source hash within each budget. It does not count unexecuted or duplicate code.
// The generic comparator uses the same typed root templates; it is intentionally
// favorable to these three rules and is not an estimate for a general fuzzer.
func Yield(b *testing.B, compile Compiler) {
	for _, directed := range []bool{false, true} {
		name := "generic-template"
		if directed {
			name = "directed"
		}
		b.Run(name, func(b *testing.B) {
			probe, err := compile(Cases()[4].Wasm(), false, Cases()[4].Rule())
			if err != nil {
				b.Fatal(err)
			}
			if probe.Selection == -1 {
				b.Skip("yield requires compiler diagnostics")
			}
			var useful, activated int64
			allInputs := Inputs()
			inputs := [][3]uint64{allInputs[3], allInputs[6]}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				seen := make(map[[32]byte]bool)
				for _, r := range Generate(Seed, directed) {
					raw := r.Wasm()
					a, err := compile(raw, false, r.Rule())
					if err != nil {
						b.Fatal(err)
					}
					if a.Selection >= 0 {
						if err = SelectionGate(a, r.Selected()); err != nil {
							b.Fatal(err)
						}
					}
					if err = execute(a, r, inputs); err != nil {
						b.Fatal(err)
					}
					if a.Selection > 0 {
						activated++
						hash := sha256.Sum256(raw)
						if !seen[hash] {
							useful++
							seen[hash] = true
						}
					}
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(Budget), "candidates/op")
			b.ReportMetric(float64(activated)/float64(b.N), "activated/op")
			b.ReportMetric(float64(useful)/float64(b.N), "useful/op")
			b.ReportMetric(float64(useful)/b.Elapsed().Seconds(), "useful/s")
		})
	}
}

// Benchmark pairs keep exact semantics for the selected fixed input. SIMD's
// control replaces the immediate with a parameter; scalar controls use existing
// kill switches. Labels distinguish this substitution from disabling a rule.
func Benchmark(b *testing.B, compile Compiler) {
	for _, r := range Cases() {
		if !r.Selected() || (r.Context != "plain" && r.Context != "live-call") {
			continue
		}
		probe, err := compile(r.Wasm(), false, r.Rule())
		if err != nil {
			b.Fatal(err)
		}
		if probe.Selection == -2 {
			continue
		}
		for _, control := range []bool{false, true} {
			name := r.Family + "/" + r.Context + "/selected"
			if control {
				name = r.Family + "/" + r.Context + "/disabled"
				if r.Family == "simd" {
					name = r.Family + "/" + r.Context + "/dynamic"
				}
			}
			raw := r.Wasm()
			disabled := control
			if control && r.Family == "simd" {
				substitute := r
				substitute.Variable = true
				raw = substitute.Wasm()
				disabled = false
			}
			b.Run("compile/"+name, func(b *testing.B) {
				var a Artifact
				var err error
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					a, err = compile(raw, disabled, r.Rule())
					if err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(a.Code)), "native-B")
			})
			b.Run("execute/"+name, func(b *testing.B) {
				a, err := compile(raw, disabled, r.Rule())
				if err != nil {
					b.Fatal(err)
				}
				if a.Selection == -2 {
					b.Skip("target has no selected rule")
				}
				e, err := prepare(a)
				if err != nil {
					b.Fatal(err)
				}
				defer e.close()
				if err = e.jm.BindTrapCell(e.trap); err != nil {
					b.Fatal(err)
				}
				base := e.jm.LinMemBase()
				in := Inputs()[3]
				if r.Family == "simd" {
					in[2] = uint64(uint32(r.Count))
				}
				e.input(in)
				if err = e.eng.CallPrepared(e.entry, e.args, base, e.trap, e.results); err != nil {
					b.Fatal(err)
				}
				if err = e.check(r, in); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err = e.eng.CallPrepared(e.entry, e.args, base, e.trap, e.results); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if err = e.check(r, in); err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(a.Code)), "native-B")
			})
		}
	}
}
