//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// TestLocalSetGetCorrectness covers the set;get -> tee peephole and the cases
// that must NOT fuse (different local, set not followed by get, plain tee),
// across both the integer (GPR) and float (XMM) local paths.
func TestLocalSetGetCorrectness(t *testing.T) {
	cases := []struct {
		name string
		wat  string
		args []int32
		want int32
	}{
		{"accum", // two adjacent set;get pairs
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 local.get 1 i32.add local.set 1
				local.get 1 i32.const 7 i32.add local.set 1
				local.get 1))`,
			[]int32{10}, 17}, // (10+0)=10, +7=17
		{"const_to_local",
			`(module (func (export "f") (result i32) (local i32)
				i32.const 42 local.set 0 local.get 0))`,
			nil, 42},
		{"set_then_get_other", // set 1 then get 0 -> must NOT fuse, must rewind
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 i32.const 5 i32.add local.set 1
				local.get 0))`,
			[]int32{3}, 3}, // local 1 = 8 (discarded), returns local 0 = 3
		{"set_then_nonget",
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 local.set 1 i32.const 99))`,
			[]int32{3}, 99},
		{"plain_tee",
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 i32.const 3 i32.mul local.tee 1 local.get 1 i32.add))`,
			[]int32{4}, 24}, // (4*3)=12, 12+12
		// the set;get fuses, then local 1 is overwritten; the kept value (the
		// original param) must be independent of the slot.
		{"set_get_then_write",
			`(module (func (export "f") (param i32) (result i32) (local i32)
				local.get 0 local.set 1
				local.get 1
				i32.const 100 local.set 1
				i32.const 1 i32.add))`,
			[]int32{7}, 8}, // kept value 7, +1 = 8; local 1 now 100 (unused)
		// float locals fuse through the XMM tee path (materializeF / FStoreDisp
		// / pushFReg); store a float const, fuse the get, confirm the kept value
		// round-trips. Result is the f*.eq, so runI32 reads a 0/1.
		{"f64_set_get",
			`(module (func (export "f") (result i32) (local f64)
				f64.const 3.5 local.set 0 local.get 0 f64.const 3.5 f64.eq))`,
			nil, 1},
		{"f32_set_get",
			`(module (func (export "f") (result i32) (local f32)
				f32.const 1.25 local.set 0 local.get 0 f32.const 1.25 f32.eq))`,
			nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := watToModule(t, tc.wat)
			if got := runI32(t, m, tc.args...); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestLocalSetGetShrinksCode proves the peephole removes the store+reload:
// the fused `set;get` body must not reload the just-stored local back into a
// register. We check there is no `mov reg,[rbp-...]` that targets the same slot
// a preceding store wrote — simplest proxy: the fused function is strictly
// smaller than the same logic with a non-fusing barrier in between.
func TestLocalSetGetShrinksCode(t *testing.T) {
	fused := watToModule(t, `(module (func (export "f") (param i32) (result i32) (local i32)
		local.get 0 local.get 1 i32.add local.set 1 local.get 1))`)
	notFused := watToModule(t, `(module (func (export "f") (param i32) (result i32) (local i32)
		local.get 0 local.get 1 i32.add local.set 1 i32.const 0 drop local.get 1))`)
	fc, err := CompileFunction(fused, 0)
	if err != nil {
		t.Fatal(err)
	}
	nc, err := CompileFunction(notFused, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(fc) >= len(nc) {
		t.Fatalf("expected fused (%d B) < non-fused (%d B)", len(fc), len(nc))
	}
	t.Logf("fused=%d B, non-fused=%d B (saved %d B)", len(fc), len(nc), len(nc)-len(fc))
}

// --- benchmark on an accumulator-heavy function ---

func watToModuleB(b *testing.B, wat string) *wasm.Module {
	b.Helper()
	w2w, err := exec.LookPath("wat2wasm")
	if err != nil {
		b.Skip("wat2wasm (wabt) not on PATH")
	}
	dir := b.TempDir()
	src := filepath.Join(dir, "m.wat")
	out := filepath.Join(dir, "m.wasm")
	if err := os.WriteFile(src, []byte(wat), 0o644); err != nil {
		b.Fatal(err)
	}
	if o, err := exec.Command(w2w, src, "-o", out).CombinedOutput(); err != nil {
		b.Fatalf("wat2wasm: %v\n%s", err, o)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		b.Fatal(err)
	}
	m, err := wasm.Decode(data)
	if err != nil {
		b.Fatal(err)
	}
	if err := wasm.Validate(m); err != nil {
		b.Fatal(err)
	}
	return m
}

// hornerWat evaluates a degree-d polynomial via Horner's method: a recurrence
// acc = acc*x + 1. Each term ends `... local.set $acc` and the next begins
// `local.get $acc ...`, so the set;get pairs fuse and acc stays resident in a
// register across the whole chain instead of round-tripping through its slot
// on the dependency-critical path.
func hornerWat(d int) string {
	var b strings.Builder
	b.WriteString(`(module (func (export "f") (param $x i32) (result i32) (local $acc i32)` + "\n")
	b.WriteString("  i32.const 1 local.set $acc\n")
	for i := 0; i < d; i++ {
		b.WriteString("  local.get $acc local.get $x i32.mul i32.const 1 i32.add local.set $acc\n")
	}
	b.WriteString("  local.get $acc))")
	return b.String()
}

func BenchmarkHornerCompile(b *testing.B) {
	m := watToModuleB(b, hornerWat(64))
	if code, err := CompileFunction(m, 0); err == nil {
		b.Logf("code size: %d bytes", len(code))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CompileFunction(m, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHornerExec(b *testing.B) {
	m := watToModuleB(b, hornerWat(64))
	code, err := CompileFunction(m, 0)
	if err != nil {
		b.Fatal(err)
	}
	eng, err := runtime.NewEngine()
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	jm, err := runtime.NewJobMemory(65536)
	if err != nil {
		b.Fatal(err)
	}
	defer jm.Close()
	ar, err := runtime.NewArena(4096)
	if err != nil {
		b.Fatal(err)
	}
	defer ar.Close()
	mem, entry, err := runtime.MapCode(code)
	if err != nil {
		b.Fatal(err)
	}
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(128)
	results := ar.Alloc(128)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint32(serArgs, 3)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
			b.Fatal(err)
		}
	}
}
