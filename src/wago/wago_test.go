//go:build linux && amd64

package wago

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm3"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// testdata loads a checked-in wasm fixture from the repo-root tests/testdata
// directory. Go runs tests with the working directory set to the package dir,
// so the fixtures live two levels up from src/wago.
func testdata(name string) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "testdata", name))
	if err != nil {
		panic(err)
	}
	return b
}

// Real AssemblyScript payloads (compiled with `asc -O3 --runtime stub`), run
// end-to-end through wago: decode -> validate -> Valent-Block compile ->
// no-cgo execute.
var (
	fibWasm     = testdata("fib.wasm")
	factWasm    = testdata("fact.wasm")
	mathopsWasm = testdata("mathops.wasm")
	recurWasm   = testdata("recur.wasm")
	logdemoWasm = testdata("logdemo.wasm")
)

func TestInvokeDynamicallySizesArgBuffer(t *testing.T) {
	params := make([]wasm.ValType, 65)
	for i := range params {
		params[i] = wasm.I32
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x40, 0x0b}))), // local.get 64
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := InstantiateWithImports(c, Imports{})
	if err != nil {
		t.Fatalf("InstantiateWithImports: %v", err)
	}
	defer in.Close()
	args := make([]Value, 65)
	for i := range args {
		args[i] = I32(int32(i))
	}
	res, err := in.Invoke("f", args...)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(res) != 1 || res[0].AsI32() != 64 {
		t.Fatalf("Invoke result = %v, want 64", res)
	}
}

func TestInvokeDynamicallySizesResultBuffer(t *testing.T) {
	results := make([]wasm.ValType, 65)
	for i := range results {
		results[i] = wasm.I32
	}
	body := make([]byte, 0, 65*2+1)
	for i := 0; i < 65; i++ {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i))...)
	}
	body = append(body, 0x0b)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := InstantiateWithImports(c, Imports{})
	if err != nil {
		t.Fatalf("InstantiateWithImports: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("f")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(res) != 65 {
		t.Fatalf("Invoke returned %d results, want 65", len(res))
	}
	for i, v := range res {
		if v.AsI32() != int32(i) {
			t.Fatalf("result %d = %d, want %d", i, v.AsI32(), i)
		}
	}
}

func run1(t *testing.T, wasm []byte, export string, args ...int32) int32 {
	t.Helper()
	res, err := Run(wasm, export, args...)
	if err != nil {
		t.Fatalf("%s%v: %v", export, args, err)
	}
	if len(res) != 1 {
		t.Fatalf("%s: expected 1 result, got %v", export, res)
	}
	return int32(res[0])
}

func TestAssemblyScriptFib(t *testing.T) {
	want := map[int32]int32{0: 0, 1: 1, 2: 1, 10: 55, 20: 6765, 30: 832040}
	for n, w := range want {
		if got := run1(t, fibWasm, "fib", n); got != w {
			t.Errorf("fib(%d) = %d, want %d", n, got, w)
		}
	}
}

func TestAssemblyScriptFact(t *testing.T) {
	want := map[int32]int32{0: 1, 1: 1, 5: 120, 10: 3628800, 12: 479001600}
	for n, w := range want {
		if got := run1(t, factWasm, "fact", n); got != w {
			t.Errorf("fact(%d) = %d, want %d", n, got, w)
		}
	}
}

func TestAssemblyScriptMathops(t *testing.T) {
	// gcd exercises the lazy-local aliasing fix (local.get/local.tee of the same
	// local before a rem_s consumes the earlier read).
	gcd := [][3]int32{{48, 36, 12}, {1071, 462, 21}, {17, 5, 1}, {100, 0, 100}, {270, 192, 6}}
	for _, c := range gcd {
		if got := run1(t, mathopsWasm, "gcd", c[0], c[1]); got != c[2] {
			t.Errorf("gcd(%d,%d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
	collatz := map[int32]int32{1: 0, 6: 8, 7: 16, 27: 111}
	for n, w := range collatz {
		if got := run1(t, mathopsWasm, "collatz", n); got != w {
			t.Errorf("collatz(%d) = %d, want %d", n, got, w)
		}
	}
	isqrt := map[int32]int32{0: 0, 1: 1, 15: 3, 16: 4, 99: 9, 100: 10}
	for n, w := range isqrt {
		if got := run1(t, mathopsWasm, "isqrt", n); got != w {
			t.Errorf("isqrt(%d) = %d, want %d", n, got, w)
		}
	}
}

// Recursion via internal calls (wasm->wasm on the foreign stack).
func TestAssemblyScriptRecursion(t *testing.T) {
	fib := map[int32]int32{0: 0, 1: 1, 10: 55, 20: 6765, 25: 75025}
	for n, w := range fib {
		if got := run1(t, recurWasm, "fibrec", n); got != w {
			t.Errorf("fibrec(%d) = %d, want %d", n, got, w)
		}
	}
	if got := run1(t, recurWasm, "sumto", 100); got != 5050 {
		t.Errorf("sumto(100) = %d, want 5050", got)
	}
	ack := [][3]int32{{0, 0, 1}, {2, 3, 9}, {3, 3, 61}, {3, 4, 125}}
	for _, c := range ack {
		res, err := Run(recurWasm, "ack", c[0], c[1])
		if err != nil {
			t.Fatalf("ack(%d,%d): %v", c[0], c[1], err)
		}
		if int32(res[0]) != c[2] {
			t.Errorf("ack(%d,%d) = %d, want %d", c[0], c[1], res[0], c[2])
		}
	}
}

// Host imports: AssemblyScript calls an imported log() which we wire to Go.
func TestAssemblyScriptHostLog(t *testing.T) {
	var logged []int32
	hosts := map[string]HostFunc{
		"logdemo.log": func(arg int32) { logged = append(logged, arg) },
	}
	if _, err := RunWithHost(logdemoWasm, hosts, "countdown", 5); err != nil {
		t.Fatal(err)
	}
	want := []int32{5, 4, 3, 2, 1, 0}
	if fmt.Sprint(logged) != fmt.Sprint(want) {
		t.Fatalf("countdown logs = %v, want %v", logged, want)
	}

	logged = nil
	res, err := RunWithHost(logdemoWasm, hosts, "sumlog", 5)
	if err != nil {
		t.Fatal(err)
	}
	if res[0] != 15 {
		t.Errorf("sumlog(5) = %d, want 15", res[0])
	}
	if want := []int32{1, 3, 6, 10, 15}; fmt.Sprint(logged) != fmt.Sprint(want) {
		t.Fatalf("sumlog logs = %v, want %v", logged, want)
	}
}

// AssemblyScript using linear memory (load/store with bounds checks).
var memprogWasm = testdata("memprog.wasm")

func TestAssemblyScriptMemory(t *testing.T) {
	// sumsq(n) = sum of i*i for i in [0,n)
	for _, c := range [][2]int32{{1, 0}, {5, 30}, {10, 285}, {20, 2470}} {
		if got := run1(t, memprogWasm, "sumsq", c[0]); got != c[1] {
			t.Errorf("sumsq(%d) = %d, want %d", c[0], got, c[1])
		}
	}
	// revfirst reverses [1..n] in memory; new element [0] == n
	for _, n := range []int32{1, 5, 100} {
		if got := run1(t, memprogWasm, "revfirst", n); got != n {
			t.Errorf("revfirst(%d) = %d, want %d", n, got, n)
		}
	}
}

// AssemblyScript using i64 (64-bit results).
var i64progWasm = testdata("i64prog.wasm")

func TestAssemblyScriptI64(t *testing.T) {
	fib64 := map[int32]int64{10: 55, 50: 12586269025, 90: 2880067194370816120}
	for n, w := range fib64 {
		res, err := Run(i64progWasm, "fib64", n)
		if err != nil {
			t.Fatal(err)
		}
		if res[0] != w {
			t.Errorf("fib64(%d) = %d, want %d", n, res[0], w)
		}
	}
	// 20! mod (1e9+7)
	res, err := Run(i64progWasm, "factmod", 20, 1000000007)
	if err != nil {
		t.Fatal(err)
	}
	if res[0] != 146326063 {
		t.Errorf("factmod(20, 1e9+7) = %d, want 146326063", res[0])
	}
}

// AssemblyScript using f64 floats.
var fprogWasm = testdata("fprog.wasm")

func TestAssemblyScriptFloat(t *testing.T) {
	res, err := Run(fprogWasm, "harmonic", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := math.Float64frombits(uint64(res[0]))
	want := 0.0
	for i := 1; i <= 10; i++ {
		want += 1.0 / float64(i)
	}
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("harmonic(10) = %v, want %v", got, want)
	}
}

func TestCompiledRoundtrip(t *testing.T) {
	c, err := Compile(fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !IsCompiled(blob) {
		t.Fatal("blob not recognized as compiled")
	}
	c2, err := Load(blob) // load precompiled, no recompile
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	res, err := in.Invoke("fib", I32(30))
	if err != nil {
		t.Fatal(err)
	}
	if res[0].AsI32() != 832040 {
		t.Fatalf("fib(30) from blob = %d, want 832040", res[0].AsI32())
	}
}

func TestCompiledOldVersionRejected(t *testing.T) {
	old := []byte{'W', 'A', 'G', 'O', wagoVersion - 1}
	if _, err := Load(old); err == nil {
		t.Fatal("Load old compiled version succeeded, want error")
	}
}

func TestRunValuesTyped(t *testing.T) {
	// f64 args + f64 result.
	r, err := RunValues(fprogWasm, "hypot", F64(3), F64(4))
	if err != nil {
		t.Fatal(err)
	}
	if got := r[0].AsF64(); got < 4.999 || got > 5.001 {
		t.Fatalf("hypot(3,4) = %v, want 5", got)
	}
	// i64 result.
	r, err = RunValues(i64progWasm, "fib64", I32(90))
	if err != nil {
		t.Fatal(err)
	}
	if r[0].AsI64() != 2880067194370816120 {
		t.Fatalf("fib64(90) = %d", r[0].AsI64())
	}
}

var indirectWasm = testdata("indirect.wasm")

func TestCallIndirect(t *testing.T) {
	c, err := Compile(indirectWasm)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, tc := range []struct {
		which, a, b, want int32
	}{{0, 7, 3, 10}, {1, 7, 3, 4}, {2, 7, 3, 21}} {
		r, err := in.Invoke("apply", I32(tc.which), I32(tc.a), I32(tc.b))
		if err != nil {
			t.Fatalf("apply(%d): %v", tc.which, err)
		}
		if r[0].AsI32() != tc.want {
			t.Errorf("apply(%d,%d,%d) = %d, want %d", tc.which, tc.a, tc.b, r[0].AsI32(), tc.want)
		}
	}
	// index 3 = neg (unop) called as binop -> signature trap
	if _, err := in.Invoke("apply", I32(3), I32(7), I32(3)); err == nil {
		t.Error("apply(3) should trap (wrong signature)")
	}
	// index 4 = out of bounds (table size 4) -> trap
	if _, err := in.Invoke("apply", I32(4), I32(7), I32(3)); err == nil {
		t.Error("apply(4) should trap (out of bounds)")
	}
}

var dispatchWasm = testdata("dispatch.wasm")

// Real AssemblyScript using indirect calls (function pointers), select, and data
// segments (the function-ref objects live in linear memory).
func TestAssemblyScriptDispatch(t *testing.T) {
	c, _ := Compile(dispatchWasm)
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, tc := range []struct{ which, want int32 }{{0, 14}, {1, 6}, {2, 40}} {
		r, err := in.Invoke("apply", I32(tc.which), I32(10), I32(4))
		if err != nil {
			t.Fatal(err)
		}
		if r[0].AsI32() != tc.want {
			t.Errorf("apply(%d,10,4) = %d, want %d", tc.which, r[0].AsI32(), tc.want)
		}
	}
}
