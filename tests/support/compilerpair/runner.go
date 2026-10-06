package compilerpair

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
)

type Artifact struct {
	HostEvents   bool
	SourceRanges int
	Code         []byte
	Entry        []int
	Shared       []bool
	Frames       []int
	FeatureMask  uint32
}
type Compiler func([]byte, bool) (Artifact, error)

func CheckPaths(got []bool, expected []bool, requestShared bool) error {
	if len(got) != len(expected) {
		return fmt.Errorf("unobserved function paths: got %d want %d", len(got), len(expected))
	}
	for i, want := range expected {
		want = want && requestShared
		if got[i] != want {
			return fmt.Errorf("function %d shared=%t want %t", i, got[i], want)
		}
	}
	return nil
}

type execution struct {
	events              []byte
	eng                 *wruntime.Engine
	memory              *wruntime.JobMemory
	arena               *wruntime.Arena
	code                []byte
	base                uintptr
	args, results, trap []byte
}

func prepare(t testing.TB, a Artifact) *execution {
	t.Helper()
	e := &execution{}
	var err error
	e.eng, err = wruntime.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.eng.Close() })
	e.memory, err = wruntime.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.memory.Close() })
	e.arena, err = wruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.arena.Close() })
	e.code, e.base, err = wruntime.MapCode(a.Code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wruntime.Unmap(e.code) })
	if !bytes.Equal(e.code[:len(a.Code)], a.Code) {
		t.Fatal("loaded native bytes differ")
	}
	if a.HostEvents {
		e.events = e.arena.Alloc(256)
		clear(e.events)
		e.memory.SetCustomCtx(uintptr(unsafe.Pointer(&e.events[0])))
	}
	e.args = e.arena.Alloc(16)
	e.results = e.arena.Alloc(16)
	e.trap = e.arena.Alloc(wruntime.TrapBufferBytes)
	return e
}
func (e *execution) call(entry int) error {
	if e.events != nil {
		clear(e.events[:8])
	}
	return e.eng.Call(e.base+uintptr(entry), e.args, e.memory.LinearMemory(), e.trap, e.results)
}
func (e *execution) check(f Fixture, i int, err error) error {
	if f.Trap[i] {
		if trap, ok := err.(*wruntime.TrapError); !ok || trap.Code != wruntime.TrapUnreachable {
			return fmt.Errorf("trap=%v want unreachable", err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	for j, want := range f.Want[i] {
		got := binary.LittleEndian.Uint64(e.results[j*8:])
		if len(f.Wide) <= i || f.Wide[i]&(1<<j) == 0 {
			got = uint64(uint32(got))
			want = uint64(uint32(want))
		}
		if got != want {
			return fmt.Errorf("result %d = %x want %x", j, got, want)
		}
	}
	if f.Events != nil {
		want := f.Events[i]
		if n := binary.LittleEndian.Uint32(e.events); n != uint32(len(want)) {
			return fmt.Errorf("event count=%d want %d", n, len(want))
		}
		for j, v := range want {
			record := e.events[8+j*8:]
			if binary.LittleEndian.Uint32(record) != 0 || binary.LittleEndian.Uint32(record[4:]) != v {
				return fmt.Errorf("ordered event %d differs", j)
			}
		}
	}
	if len(f.Memory) != 0 && !bytes.Equal(e.memory.LinearMemory()[:len(f.Memory)], f.Memory) {
		return fmt.Errorf("visible memory differs")
	}
	return nil
}

func RunMatrix(t *testing.T, compile Compiler, diagnostics bool) {
	fixtures := Fixtures()
	for _, f := range fixtures {
		t.Run(f.ID, func(t *testing.T) {
			m, err := wasm.DecodeModule(f.Wasm)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			if f.Invalid != nil {
				bad, err := wasm.DecodeModule(f.Invalid)
				if err != nil {
					t.Fatalf("fault must reach validation: %v", err)
				}
				if err := wasm.ValidateModule(bad); err == nil {
					t.Fatal("one-premise invalid case accepted")
				} else {
					t.Logf("validator rejected paired premise: %v", err)
				}
			}
			var baseline Artifact
			for _, requested := range []bool{false, true} {
				a, err := compile(f.Wasm, requested)
				if err != nil {
					t.Fatalf("shared=%t compile: %v", requested, err)
				}
				if len(a.Entry) != len(f.Want) {
					t.Fatalf("compiled function count=%d want %d", len(a.Entry), len(f.Want))
				}
				if diagnostics {
					if err := CheckPaths(a.Shared, f.Shared, requested); err != nil {
						t.Fatal(err)
					}
				}
				e := prepare(t, a)
				completed := 0
				for i, entry := range a.Entry {
					if err := e.check(f, i, e.call(entry)); err != nil {
						t.Fatalf("shared=%t f%d: %v", requested, i, err)
					}
					completed++
				}
				if err := checkExecutionCount(completed, len(f.Want)); err != nil {
					t.Fatal(err)
				}
				t.Logf("Go=%s target=%s/%s requested-shared=%t observed=%v features=%x bounds=explicit API=Engine.Call wasm=%x loaded=%x native-B=%d frames=%v premise=%s", runtime.Version(), runtime.GOOS, runtime.GOARCH, requested, a.Shared, a.FeatureMask, sha256.Sum256(f.Wasm), sha256.Sum256(e.code[:len(a.Code)]), len(a.Code), a.Frames, f.Reason)
				if !requested {
					baseline = a
				} else if diagnostics {
					hasShared := false
					for _, admitted := range f.Shared {
						hasShared = hasShared || admitted
					}
					if hasShared && CheckPaths(baseline.Shared, f.Shared, true) == nil {
						t.Fatal("disabled-shared control passed requested-shared gate")
					}
				}
			}

			if f.ID == "many-functions" {
				e := prepare(t, baseline)
				completed := 0
				for i, entry := range baseline.Entry {
					var err error
					if i != 0 {
						err = e.call(entry)
						completed++
					} // f0 returns zero, as the untouched buffer does.
					if err := e.check(f, i, err); err != nil {
						t.Fatal(err)
					}
				}
				if checkExecutionCount(completed, len(f.Want)) == nil {
					t.Fatal("omitted zero-result execution passed coverage gate")
				}
			}
		})
	}
	t.Run("independent-v8", func(t *testing.T) { independent(t, fixtures) })
}

type nodeFeatureProbe struct {
	Name string
	Wasm []byte
}

func independentFeatureProbes() []nodeFeatureProbe {
	return []nodeFeatureProbe{
		{"exception-handling (try_table)", module([]function{{body: []byte{0, 0x1f, 0x40, 0, 0x0b, 0x0b}}}, nil, false)},
		{"GC (i31)", module([]function{{results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x41, 0, 0xfb, 0x1c, 0xfb, 0x1d, 0x0b}}}, nil, false)},
	}
}

// The reference engine receives the exact same Wasm bytes, with no expected
// answers in its script. Missing Node or proposals are separate skips.
func independent(t *testing.T, fixtures []Fixture) {
	independentWithProbes(t, fixtures, independentFeatureProbes())
}

func independentWithProbes(t *testing.T, fixtures []Fixture, probes []nodeFeatureProbe) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node/V8 independent engine unavailable")
	}
	type input struct {
		ID        string
		Wasm      []byte
		Functions int
		Memory    bool
	}
	inputs := make([]input, len(fixtures))
	for i, f := range fixtures {
		inputs[i] = input{f.ID, f.Wasm, len(f.Want), len(f.Memory) != 0}
	}
	payload, err := json.Marshal(struct {
		Probes   []nodeFeatureProbe
		Fixtures []input
	}{probes, inputs})
	if err != nil {
		t.Fatal(err)
	}
	script := `
let data = '';
process.stdin.on('data', b => data += b);
process.stdin.on('end', async () => {
  try {
    const {Probes, Fixtures} = JSON.parse(data);
    const unsupported = Probes.filter(p => !WebAssembly.validate(Buffer.from(p.Wasm, 'base64'))).map(p => p.Name);
    if (unsupported.length) {
      console.log(JSON.stringify({node: process.version, v8: process.versions.v8, unsupported}));
      return;
    }
    const rows = [];
    for (const f of Fixtures) {
      const raw = Buffer.from(f.Wasm, 'base64');
      const events = [];
      const {instance} = await WebAssembly.instantiate(raw, {env: {event: x => events.push(x)}});
      const values = [];
      for (let i = 0; i < f.Functions; i++) {
        try {
          events.length = 0;
          const x = instance.exports['f' + i]();
          values.push({values: (x === undefined ? [] : Array.isArray(x) ? x : [x]).map(String), trap: false, events: events.slice()});
        } catch (e) {
          if (!(e instanceof WebAssembly.RuntimeError)) throw e;
          values.push({values: [], trap: true});
        }
      }
      rows.push({id: f.ID, values, memory: f.Memory ? Array.from(new Uint8Array(instance.exports.memory.buffer, 0, 4)) : []});
    }
    console.log(JSON.stringify({node: process.version, v8: process.versions.v8, rows}));
  } catch (e) {
    console.error(e);
    process.exitCode = 1;
  }
});`

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", script)
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("independent V8: %v %s", err, out)
	}
	var report struct {
		Node, V8    string
		Unsupported []string
		Rows        []struct {
			ID     string
			Values []struct {
				Values []string
				Trap   bool
				Events []uint32
			}
			Memory []byte
		}
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Unsupported) != 0 {
		t.Skipf("Node %s / V8 %s lacks required Wasm features: %v", report.Node, report.V8, report.Unsupported)
	}
	if len(report.Rows) != len(fixtures) {
		t.Fatal("independent engine omitted a fixture")
	}
	for i, f := range fixtures {
		row := report.Rows[i]
		if row.ID != f.ID || len(row.Values) != len(f.Want) {
			t.Fatal("independent engine returned wrong case identity/count")
		}
		for j, got := range row.Values {
			if got.Trap != f.Trap[j] || len(got.Values) != len(f.Want[j]) {
				t.Fatalf("V8 %s f%d: %+v", f.ID, j, got)
			}
			if f.Events != nil {
				want := f.Events[j]
				if len(got.Events) != len(want) {
					t.Fatalf("V8 %s f%d omitted events", f.ID, j)
				}
				for k, v := range want {
					if got.Events[k] != v {
						t.Fatalf("V8 %s f%d event order", f.ID, j)
					}
				}
			}
			for k, v := range got.Values {
				bits, err := strconv.ParseInt(v, 10, 64)
				got, want := uint64(bits), f.Want[j][k]
				if len(f.Wide) <= j || f.Wide[j]&(1<<k) == 0 {
					got = uint64(uint32(got))
					want = uint64(uint32(want))
				}
				if err != nil || got != want {
					t.Fatalf("V8 %s f%d = %s want %d", f.ID, j, v, f.Want[j][k])
				}
			}
		}
		if len(f.Memory) != 0 && !bytes.Equal(row.Memory, f.Memory) {
			t.Fatalf("V8 %s memory=%x", f.ID, row.Memory)
		}
	}
	t.Logf("independent engine %s V8=%s fixtures=%d", report.Node, report.V8, len(report.Rows))
}

func BenchmarkCompile(b *testing.B, compile Compiler) {
	for _, f := range Fixtures() {
		for _, requested := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/shared=%t", f.ID, requested), func(b *testing.B) {
				a, err := compile(f.Wasm, requested)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := compile(f.Wasm, requested); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(len(a.Code)), "native-B")
			})
		}
	}
}
func BenchmarkExecute(b *testing.B, compile Compiler) {
	for _, f := range Fixtures() {
		if f.Trap[0] {
			continue
		}
		for _, requested := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/shared=%t", f.ID, requested), func(b *testing.B) {
				a, err := compile(f.Wasm, requested)
				if err != nil {
					b.Fatal(err)
				}
				e := prepare(b, a)
				if err := e.check(f, 0, e.call(a.Entry[0])); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := e.call(a.Entry[0]); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(len(a.Code)), "native-B")
				if err := e.check(f, 0, e.call(a.Entry[0])); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

func RunSourceFallback(t *testing.T, compile Compiler) {
	f := Fixtures()[0]
	for _, requested := range []bool{false, true} {
		a, err := compile(f.Wasm, requested)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckPaths(a.Shared, f.Shared, false); err != nil {
			t.Fatal(err)
		}
		if a.SourceRanges == 0 {
			t.Fatal("fixture did not enable source recording")
		}
		if CheckPaths(a.Shared, f.Shared, true) == nil {
			t.Fatal("profile fallback counted as shared coverage")
		}
		e := prepare(t, a)
		if err := e.check(f, 0, e.call(a.Entry[0])); err != nil {
			t.Fatal(err)
		}
		t.Logf("requested-shared=%t actual=established source-ranges=%d", requested, a.SourceRanges)
	}
}

func checkExecutionCount(completed, scheduled int) error {
	if scheduled <= 0 || completed != scheduled {
		return fmt.Errorf("completed %d function calls, want %d", completed, scheduled)
	}
	return nil
}
