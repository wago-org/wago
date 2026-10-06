//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
)

const nestedHostWidth = 48

func nestedHostModule(t testing.TB) []byte {
	t.Helper()
	types := strings.TrimSpace(strings.Repeat("i32 i64 f32 f64 ", nestedHostWidth/4))
	var args strings.Builder
	for i := 0; i < nestedHostWidth; i++ {
		fmt.Fprintf(&args, " local.get %d", i)
	}
	return watToWasm(t, fmt.Sprintf(`(module
 (type $indirect (func (result i32)))
 (import "env" "step" (func $step (param %s) (result %s)))
 (memory (export "memory") 1 2)
 (global $g (mut i32) (i32.const 0))
 (table 1 funcref)
 (func $old (type $indirect) i32.const 33)
 (func $new (type $indirect) i32.const 77)
 (elem (i32.const 0) $old)
 (elem declare func $new)
 (func (export "init") (param i32)
  i32.const 0 local.get 0 i32.store
  local.get 0 i32.const 1 i32.add global.set $g)
 (func (export "mutate") (param $mark i32) (param $trap i32)
  memory.size i32.const 1 i32.eq
  if i32.const 1 memory.grow drop end
  i32.const 65536 local.get $mark i32.store
  local.get $mark i32.const 1 i32.add global.set $g
  i32.const 0 ref.func $new table.set
  local.get $trap if unreachable end)
 (func $observe (export "observe") (result i32 i32 i32 i32 i32)
  i32.const 0 i32.load memory.size
  memory.size i32.const 2 i32.eq
  if (result i32) i32.const 65536 i32.load else i32.const 0 end
  global.get $g i32.const 0 call_indirect (type $indirect))
 (func (export "run") (param %s) (result %s i32 i32 i32 i32 i32)
  %s call $step call $observe))`, types, types, types, types, args.String()))
}

// i32/f32 values occupy the low 32 bits of an ABI slot; the high bits are
// unspecified. Compare all semantic bits without imposing zero extension.
func nestedHostSlotBits(i int, bits uint64) uint64 {
	if i%4 == 0 || i%4 == 2 {
		return uint64(uint32(bits))
	}
	return bits
}

func nestedHostBits(i, value int) uint64 {
	switch i % 4 {
	case 0:
		return I32(int32(value))
	case 1:
		return I64(int64(value))
	case 2:
		return F32(float32(value))
	default:
		return F64(float64(value))
	}
}

// Compare every value, including the instance marker. A valid but wrong
// instance must not pass because its output has the same shape.
func checkNestedHostState(got []uint64, want [5]uint64) error {
	if len(got) != len(want) {
		return fmt.Errorf("state result count: %d", len(got))
	}
	for i, name := range []string{"instance marker", "memory pages", "new-page marker", "global", "indirect result"} {
		if got[i] != want[i] {
			return fmt.Errorf("%s: got %d want %d", name, got[i], want[i])
		}
	}
	return nil
}

type nestedHostFixture struct {
	root, target    *Instance
	run             *WasmFunc
	args            [nestedHostWidth]uint64
	input, mark     int
	trap, different bool
	calls           int
}

func newNestedHostFixture(t testing.TB, different, trap bool) *nestedHostFixture {
	t.Helper()
	f := &nestedHostFixture{different: different, trap: trap, input: 10, mark: 400}
	raw := nestedHostModule(t)
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1), append([]byte(nil), raw...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	callback := CallerHostCallFunc(func(caller Caller, call HostCall) {
		fail := func(err error) {
			if err != nil {
				panic(HostTrap{Err: err})
			}
		}
		checkParams := func() error {
			if call.ParamCount() != nestedHostWidth || call.ResultCount() != nestedHostWidth {
				return fmt.Errorf("wide signature changed")
			}
			for i := 0; i < nestedHostWidth; i++ {
				got, _ := call.RawParam(i)
				if nestedHostSlotBits(i, got) != nestedHostBits(i, f.input+i) {
					return fmt.Errorf("parameter %d: got %x", i, got)
				}
			}
			return nil
		}
		fail(checkParams())
		// Discard the memory view before re-entry. Reacquire it after growth.
		if len(caller.Memory()) < 4 || binary.LittleEndian.Uint32(caller.Memory()) != 100 {
			fail(fmt.Errorf("caller marker before nested call"))
		}
		flag := I32(0)
		if f.trap {
			flag = I32(1)
		}
		_, err := f.target.InvokeFromHost(context.Background(), caller, "mutate", I32(int32(f.mark)), flag)
		if f.trap {
			var tr *wruntime.TrapError
			if !errors.As(err, &tr) || tr.Code != wruntime.TrapUnreachable {
				fail(fmt.Errorf("nested trap: %v", err))
			}
		} else {
			fail(err)
		}
		fail(checkParams())
		mem := caller.Memory()
		wantPages := 2
		if f.different {
			wantPages = 1
		}
		if len(mem) != wantPages*65536 || binary.LittleEndian.Uint32(mem) != 100 {
			fail(fmt.Errorf("caller memory after nested call"))
		}
		if !f.different && binary.LittleEndian.Uint32(mem[65536:]) != uint32(f.mark) {
			fail(fmt.Errorf("caller new-page marker"))
		}
		for i := 0; i < nestedHostWidth; i++ {
			// These finite values are exactly representable for both float widths.
			value := f.input + i + 1000
			switch i % 4 {
			case 0:
				call.SetI32(i, int32(value))
			case 1:
				call.SetI64(i, int64(value))
			case 2:
				call.SetF32(i, float32(value))
			case 3:
				call.SetF64(i, float64(value))
			}
		}
		f.calls++
	})
	instantiate := func(id int) *Instance {
		in, err := Instantiate(c, testImports("env.step", callback))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { in.Close() })
		if _, err := in.Invoke("init", I32(int32(id))); err != nil {
			t.Fatal(err)
		}
		return in
	}
	f.root = instantiate(100)
	f.target = f.root
	if different {
		f.target = instantiate(200)
	}
	f.run, err = f.root.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if !f.root.syncMode || f.run.paramSlots != 48 || f.run.resultSlots != 53 {
		t.Fatal("unexpected public call path")
	}
	// This wide caller-aware route cannot use the fixed typed scalar portal.
	if _, ok := f.root.syncHosts[0].typedScalarSlots(); ok {
		t.Fatal("wide call unexpectedly uses typed scalar slots")
	}
	cache := c.codeCache
	if cache == nil || cache.base != f.root.base || len(cache.mem) < len(c.code) {
		t.Fatal("loaded code image unavailable")
	}
	qualifyNestedHostCompiler(t, raw, cache.mem[:len(c.code)])
	t.Logf("Go=%s target=%s/%s API=CallerHostCallFunc/WasmFunc.Invoke bounds=explicit typed-scalar=false wasm=%x loaded=%x", runtime.Version(), runtime.GOOS, runtime.GOARCH, sha256.Sum256(raw), sha256.Sum256(cache.mem[:len(c.code)]))
	return f
}

func qualifyNestedHostCompiler(t testing.TB, raw, loaded []byte) {
	t.Helper()
	if !compilerTelemetryEnabled {
		return
	}
	m, err := wasm.DecodeModule(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	slots, err := moduleSyncHostSlotCapacity(m)
	if err != nil {
		t.Fatal(err)
	}
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1)
	var stats railshotModuleStats
	cm, err := railshotCompileModuleWith(m, railshotCompileOptions{
		Workers: 1, DeferCodeMapping: true, SyncHostSlots: slots,
		Optimizations: cfg.optimizations, OptimizationSnapshot: cfg.optimizationSnapshot, OptimizationDeltas: cfg.optimizationDeltas,
		ImportBindings:   []railshotImportBinding{{Dynamic: true, ImportIndex: 0}},
		BitCountFeatures: bitCountHostFeaturesSupported(), Interruptible: !wruntime.HostInterruptSupported(), Stats: &stats,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if !bytes.Equal(cm.Code, loaded) {
		t.Fatal("diagnostic compiler differs from loaded code")
	}
	// run is the final defined function; the wide mixed host call uses the
	// established compiler. Other small functions may use the shared compiler.
	if len(stats.Funcs) != 6 || stats.Funcs[5].SharedScalar {
		t.Fatal("unexpected run compiler path")
	}
}

func (f *nestedHostFixture) invoke() ([nestedHostWidth + 5]uint64, error) {
	for i := range f.args {
		f.args[i] = nestedHostBits(i, f.input+i)
	}
	out, err := f.run.Invoke(f.args[:]...)
	var saved [nestedHostWidth + 5]uint64
	if err != nil {
		return saved, err
	}
	if len(out) != len(saved) {
		return saved, fmt.Errorf("result count=%d", len(out))
	}
	copy(saved[:], out) // The next call on this instance expires out.
	for i := 0; i < nestedHostWidth; i++ {
		if nestedHostSlotBits(i, saved[i]) != nestedHostBits(i, f.input+i+1000) {
			return saved, fmt.Errorf("result %d: got %x", i, saved[i])
		}
	}
	want := [5]uint64{100, 2, uint64(f.mark), uint64(f.mark + 1), 77}
	if f.different {
		want = [5]uint64{100, 1, 0, 101, 33}
	}
	return saved, checkNestedHostState(saved[nestedHostWidth:], want)
}

func TestHostNestedMixedState(t *testing.T) {
	for _, different := range []bool{false, true} {
		for _, trap := range []bool{false, true} {
			t.Run(fmt.Sprintf("different=%v/trap=%v", different, trap), func(t *testing.T) {
				f := newNestedHostFixture(t, different, trap)
				first, err := f.invoke()
				if err != nil {
					t.Fatal(err)
				}
				f.input = 70
				f.mark = 500
				if _, err := f.invoke(); err != nil {
					t.Fatal(err)
				}
				if f.calls != 2 {
					t.Fatalf("callbacks=%d", f.calls)
				}
				for i := 0; i < nestedHostWidth; i++ {
					if nestedHostSlotBits(i, first[i]) != nestedHostBits(i, 1010+i) {
						t.Fatalf("saved result %d changed", i)
					}
				}
				id := uint64(100)
				if different {
					id = 200
				}
				out, err := f.target.Invoke("observe")
				if err != nil {
					t.Fatal(err)
				}
				if err := checkNestedHostState(out, [5]uint64{id, 2, 500, 501, 77}); err != nil {
					t.Fatal(err)
				}
				if different {
					// B is valid and was checked above. It must fail A's observation gate.
					err := checkNestedHostState(out, [5]uint64{100, 1, 0, 101, 33})
					if err == nil || !strings.HasPrefix(err.Error(), "instance marker:") {
						t.Fatalf("wrong valid binding control: %v", err)
					}
				}
			})
		}
	}
}

// Measures the complete checked fixture after its first growth. Observer work
// and nested mutation are included; compile, instantiate, and growth are not.
func BenchmarkHostNestedMixedState(b *testing.B) {
	for _, different := range []bool{false, true} {
		for _, trap := range []bool{false, true} {
			b.Run(fmt.Sprintf("different=%v/trap=%v", different, trap), func(b *testing.B) {
				f := newNestedHostFixture(b, different, trap)
				if _, err := f.invoke(); err != nil {
					b.Fatal(err)
				}
				f.calls = 0
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := f.invoke(); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				if f.calls != b.N {
					b.Fatalf("callbacks=%d want %d", f.calls, b.N)
				}
			})
		}
	}
}
