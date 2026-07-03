//go:build linux && amd64

package wago

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/gc"
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

func TestCompileWithConfigDoesNotGateOnIRLowering(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})), // memory min 1 page
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x41, 0x00, // i32.const dst
			0x41, 0x00, // i32.const src
			0x41, 0x00, // i32.const len
			0xfc, 0x0a, 0x00, 0x00, // memory.copy 0, 0 (not lowered by IR yet)
			0x0b,
		}))),
	)
	if _, err := CompileWithConfig(NewRuntimeConfig(), mod); err != nil {
		t.Fatalf("CompileWithConfig should use direct backend without IR gate: %v", err)
	}
}

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
	in, err := Instantiate(c, Imports{})
	if err != nil {
		t.Fatalf("InstantiateWithImports: %v", err)
	}
	defer in.Close()
	args := make([]uint64, 65)
	for i := range args {
		args[i] = I32(int32(i))
	}
	res, err := in.Invoke("f", args...)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(res) != 1 || AsI32(res[0]) != 64 {
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
	in, err := Instantiate(c, Imports{})
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
		if AsI32(v) != int32(i) {
			t.Fatalf("result %d = %d, want %d", i, AsI32(v), i)
		}
	}
}

// runv compiles, instantiates with no imports, and invokes an export.
func runv(t *testing.T, wasm []byte, export string, args ...uint64) []uint64 {
	t.Helper()
	return runImports(t, wasm, Imports{}, export, args...)
}

// run1 invokes an export taking i32 args and returning one i32.
func run1(t *testing.T, wasm []byte, export string, args ...int32) int32 {
	t.Helper()
	vals := make([]uint64, len(args))
	for i, a := range args {
		vals[i] = I32(a)
	}
	res := runv(t, wasm, export, vals...)
	if len(res) != 1 {
		t.Fatalf("%s: expected 1 result, got %v", export, res)
	}
	return AsI32(res[0])
}

// runImports compiles, instantiates with imports, and invokes an export — the
// pipeline for one-shot runs that need host functions or imported globals.
func runImports(t *testing.T, wasm []byte, imports Imports, export string, args ...uint64) []uint64 {
	t.Helper()
	c, err := Compile(wasm)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, imports)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke(export, args...)
	if err != nil {
		t.Fatalf("%s: %v", export, err)
	}
	return res
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
		res := runv(t, recurWasm, "ack", I32(c[0]), I32(c[1]))
		if AsI32(res[0]) != c[2] {
			t.Errorf("ack(%d,%d) = %d, want %d", c[0], c[1], AsI32(res[0]), c[2])
		}
	}
}

// Host imports: AssemblyScript calls an imported log() which we wire to Go.
func TestAssemblyScriptHostLog(t *testing.T) {
	var logged []int32
	hosts := Imports{
		"logdemo.log": HostFunc(func(arg int32) { logged = append(logged, arg) }),
	}
	runImports(t, logdemoWasm, hosts, "countdown", I32(5))
	want := []int32{5, 4, 3, 2, 1, 0}
	if fmt.Sprint(logged) != fmt.Sprint(want) {
		t.Fatalf("countdown logs = %v, want %v", logged, want)
	}

	logged = nil
	res := runImports(t, logdemoWasm, hosts, "sumlog", I32(5))
	if AsI32(res[0]) != 15 {
		t.Errorf("sumlog(5) = %d, want 15", AsI32(res[0]))
	}
	if want := []int32{1, 3, 6, 10, 15}; fmt.Sprint(logged) != fmt.Sprint(want) {
		t.Fatalf("sumlog logs = %v, want %v", logged, want)
	}
}

// Multi-param host import: AssemblyScript's runtime imports
// env.abort(msg, file, line, col) — four i32 args, no result. wago's
// log-and-replay host-call model captures only the first arg; verify such an
// import compiles, runs, and replays that first arg. This is what gates running
// real AS modules (e.g. json-as) on wago.
func TestMultiParamHostImport(t *testing.T) {
	// types: 0 = (i32,i32,i32,i32)->(), 1 = ()->(i32)
	abortType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, nil)
	pingType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("abort")...)
	importEntry = append(importEntry, 0x00)                // func import
	importEntry = append(importEntry, wasmtest.ULEB(0)...) // of type 0
	body := []byte{
		0x41, 0x0b, // i32.const 11  (msg)
		0x41, 0x16, // i32.const 22  (file)
		0x41, 0x21, // i32.const 33  (line)
		0x41, 0x2c, // i32.const 44  (col)
		0x10, 0x00, // call 0 (abort import)
		0x41, 0x07, // i32.const 7
		0x0b, // end
	}
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(abortType, pingType)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), // func 1 : type 1
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("ping", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	var captured []int32
	hosts := Imports{"env.abort": HostFunc(func(arg int32) { captured = append(captured, arg) })}
	res := runImports(t, mod, hosts, "ping")
	if AsI32(res[0]) != 7 {
		t.Fatalf("ping() = %d, want 7", AsI32(res[0]))
	}
	if want := []int32{11}; fmt.Sprint(captured) != fmt.Sprint(want) {
		t.Fatalf("abort first-arg capture = %v, want %v", captured, want)
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
		res := runv(t, i64progWasm, "fib64", I32(n))
		if AsI64(res[0]) != w {
			t.Errorf("fib64(%d) = %d, want %d", n, AsI64(res[0]), w)
		}
	}
	// 20! mod (1e9+7)
	res := runv(t, i64progWasm, "factmod", I64(20), I64(1000000007))
	if AsI64(res[0]) != 146326063 {
		t.Errorf("factmod(20, 1e9+7) = %d, want 146326063", AsI64(res[0]))
	}
}

// AssemblyScript using f64 floats.
var fprogWasm = testdata("fprog.wasm")

func TestAssemblyScriptFloat(t *testing.T) {
	res := runv(t, fprogWasm, "harmonic", I32(10))
	got := AsF64(res[0])
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
	if gc.HasHeapObjectTypes(c.GCTypeDescs) {
		t.Fatalf("MVP compile produced heap-object GC descriptors: %+v", c.GCTypeDescs)
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
	if AsI32(res[0]) != 832040 {
		t.Fatalf("fib(30) from blob = %d, want 832040", AsI32(res[0]))
	}
}

func TestCompiledRoundtripPreservesDebugNames(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("imp")...)
	importEntry = append(importEntry, 0x00) // func import
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	namePayload := append([]byte{}, wasmtest.NameSubsection(0, wasmtest.Name("mod"))...)
	namePayload = append(namePayload, wasmtest.NameSubsection(1, wasmtest.NameMap(
		wasmtest.NameAssoc{Index: 0, Name: "imported"},
		wasmtest.NameAssoc{Index: 1, Name: ""},
	))...)
	namePayload = append(namePayload, wasmtest.NameSubsection(2, wasmtest.IndirectNameMap(
		wasmtest.IndirectNameAssoc{Index: 1, Names: []wasmtest.NameAssoc{{Index: 0, Name: "local0"}}},
	))...)
	mod := wasmtest.Module(
		wasmtest.Custom("name", namePayload),
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("z", 0, 1),
			wasmtest.ExportEntry("a", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x2a, 0x0b}))),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.Names == nil || c.Names.ModuleName == nil || *c.Names.ModuleName != "mod" {
		t.Fatalf("compiled names not preserved: %#v", c.Names)
	}
	if got, ok := c.FuncName(0); !ok || got != "imported" {
		t.Fatalf("FuncName(0) = %q, %v; want imported, true", got, ok)
	}
	if got, ok := c.LocalFuncName(0); !ok || got != "" {
		t.Fatalf("LocalFuncName(0) = %q, %v; want empty name present", got, ok)
	}
	if got, ok := c.Names.LocalName(1, 0); !ok || got != "local0" {
		t.Fatalf("LocalName(1, 0) = %q, %v; want local0, true", got, ok)
	}
	if got := c.FuncDebugName(0); got != "imported" {
		t.Fatalf("FuncDebugName(0) = %q, want imported", got)
	}
	if got := c.FuncDebugName(1); got != "a" {
		t.Fatalf("FuncDebugName(1) = %q, want stable export fallback a", got)
	}
	if got := c.FuncDebugName(99); got != "func99" {
		t.Fatalf("FuncDebugName(99) = %q, want func99", got)
	}

	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	loaded, err := Load(blob)
	if err != nil {
		t.Fatalf("Load compiled: %v", err)
	}
	if got, ok := loaded.FuncName(0); !ok || got != "imported" {
		t.Fatalf("loaded FuncName(0) = %q, %v; want imported, true", got, ok)
	}
	if got, ok := loaded.LocalFuncName(0); !ok || got != "" {
		t.Fatalf("loaded LocalFuncName(0) = %q, %v; want empty name present", got, ok)
	}
	if got := loaded.FuncDebugName(1); got != "a" {
		t.Fatalf("loaded FuncDebugName(1) = %q, want a", got)
	}
}

func TestCompiledOldVersionRejected(t *testing.T) {
	old := []byte{'W', 'A', 'G', 'O', wagoVersion - 1}
	if _, err := Load(old); err == nil {
		t.Fatal("Load old compiled version succeeded, want error")
	}
}

func representativeGCTypeDescs(t *testing.T) []gc.TypeDesc {
	t.Helper()
	pf, err := gc.NewStructDesc(1, []gc.StorageKind{gc.StorageI32, gc.StorageI64})
	if err != nil {
		t.Fatal(err)
	}
	pr, err := gc.NewStructDesc(2, []gc.StorageKind{gc.StorageRef, gc.StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	arrI32, err := gc.NewArrayDesc(3, gc.StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	arrI32.Final = false
	arrRef, err := gc.NewArrayDesc(4, gc.StorageRefNull)
	if err != nil {
		t.Fatal(err)
	}
	arrRef.HasSuper = true
	arrRef.Super = 3
	return []gc.TypeDesc{{ID: 0, Kind: gc.KindFunc, Final: true}, pf, pr, arrI32, arrRef}
}

func TestCompiledGCTypeDescsRoundTrip(t *testing.T) {
	c := &Compiled{GCTypeDescs: representativeGCTypeDescs(t)}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if blob[4] != wagoVersion {
		t.Fatalf("version byte = %d, want %d", blob[4], wagoVersion)
	}
	var out Compiled
	if err := out.UnmarshalBinary(blob); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.GCTypeDescs, c.GCTypeDescs) {
		t.Fatalf("GCTypeDescs mismatch after round trip\n got: %#v\nwant: %#v", out.GCTypeDescs, c.GCTypeDescs)
	}
}

func TestCompiledUnmarshalRejectsMalformedGCTypeSuperMetadata(t *testing.T) {
	finalBase, err := gc.NewStructDesc(0, []gc.StorageKind{gc.StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	childOfFinal, err := gc.NewStructDesc(1, []gc.StorageKind{gc.StorageRef})
	if err != nil {
		t.Fatal(err)
	}
	childOfFinal.HasSuper = true
	childOfFinal.Super = 0
	arrayBase, err := gc.NewArrayDesc(0, gc.StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	arrayBase.Final = false
	structExtendsArray, err := gc.NewStructDesc(1, []gc.StorageKind{gc.StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	structExtendsArray.HasSuper = true
	structExtendsArray.Super = 0
	structBase, err := gc.NewStructDesc(0, []gc.StorageKind{gc.StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	structBase.Final = false
	arrayExtendsStruct, err := gc.NewArrayDesc(1, gc.StorageI32)
	if err != nil {
		t.Fatal(err)
	}
	arrayExtendsStruct.HasSuper = true
	arrayExtendsStruct.Super = 0

	cases := []struct {
		name string
		desc []gc.TypeDesc
	}{
		{"final super", []gc.TypeDesc{finalBase, childOfFinal}},
		{"struct extends array", []gc.TypeDesc{arrayBase, structExtendsArray}},
		{"array extends struct", []gc.TypeDesc{structBase, arrayExtendsStruct}},
		{"heap extends func", []gc.TypeDesc{{ID: 0, Kind: gc.KindFunc}, structExtendsArray}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := (&Compiled{GCTypeDescs: tc.desc}).MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			var out Compiled
			if err := out.UnmarshalBinary(blob); err == nil {
				t.Fatal("UnmarshalBinary accepted malformed GC super metadata")
			}
		})
	}
}

func TestCompiledValidateGCTypeDescFailures(t *testing.T) {
	cases := []struct {
		name string
		desc []gc.TypeDesc
	}{
		{"id mismatch", []gc.TypeDesc{{ID: 1, Kind: gc.KindFunc}}},
		{"invalid super", []gc.TypeDesc{{ID: 0, Kind: gc.KindFunc, HasSuper: true, Super: 9}}},
		{"invalid kind", []gc.TypeDesc{{ID: 0, Kind: 99}}},
		{"invalid ref offset", []gc.TypeDesc{{ID: 0, Kind: gc.KindStruct, Fields: []gc.FieldDesc{{Kind: gc.StorageRef, Offset: 8}}, Size: 4, Align: 4, HasRefs: true}}},
		{"malformed func", []gc.TypeDesc{{ID: 0, Kind: gc.KindFunc, Size: 4}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Compiled{GCTypeDescs: tc.desc}
			if err := c.validate(); err == nil {
				t.Fatal("expected validate error")
			}
		})
	}
}

func TestInstantiateGCCollectorLifecycle(t *testing.T) {
	c, err := Compile(fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.gc != nil {
		t.Fatal("function-only/MVP module unexpectedly created GC collector")
	}
	in.Close()

	funcOnly := *c
	funcOnly.GCTypeDescs = []gc.TypeDesc{{ID: 0, Kind: gc.KindFunc}}
	in, err = Instantiate(&funcOnly, nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.gc != nil {
		t.Fatal("function-only descriptors unexpectedly created GC collector")
	}
	in.Close()

	c.GCTypeDescs = representativeGCTypeDescs(t)
	in, err = Instantiate(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.gc == nil {
		t.Fatal("GC descriptor module did not create collector")
	}
	in.Close()
}

func TestInstantiateWithOptionsGCConfig(t *testing.T) {
	c, err := Compile(fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	c.GCTypeDescs = representativeGCTypeDescs(t)
	in, err := InstantiateWithOptions(c, InstantiateOptions{GC: gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}})
	if err != nil {
		t.Fatal(err)
	}
	if in.gc == nil || in.gc.Stats().LiveObjects != 0 {
		t.Fatal("tiny GC collector was not created")
	}
	if _, err := in.gc.NewStructDefault(1); err != nil {
		t.Fatalf("tiny GC config was not usable: %v", err)
	}
	in.Close()

	funcOnly := *c
	funcOnly.GCTypeDescs = []gc.TypeDesc{{ID: 0, Kind: gc.KindFunc}}
	in, err = InstantiateWithOptions(&funcOnly, InstantiateOptions{GC: gc.Config{Profile: gc.ProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}})
	if err != nil {
		t.Fatal(err)
	}
	if in.gc != nil {
		t.Fatal("function-only descriptors unexpectedly allocated collector with options")
	}
	in.Close()
}

func TestRunValuesTyped(t *testing.T) {
	// f64 args + f64 result.
	r := runv(t, fprogWasm, "hypot", F64(3), F64(4))
	if got := AsF64(r[0]); got < 4.999 || got > 5.001 {
		t.Fatalf("hypot(3,4) = %v, want 5", got)
	}
	// i64 result.
	r = runv(t, i64progWasm, "fib64", I32(90))
	if AsI64(r[0]) != 2880067194370816120 {
		t.Fatalf("fib64(90) = %d", AsI64(r[0]))
	}
}

var indirectWasm = testdata("indirect.wasm")

func TestCallIndirectZeroLengthTableTrapsOOB(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x00})), // funcref table min 0
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}))),
	)
	c, err := Compile(mod)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !c.HasTable || c.TableSize != 0 {
		t.Fatalf("compiled table shape = HasTable %v, TableSize %d; want true, 0", c.HasTable, c.TableSize)
	}
	in, err := Instantiate(c, nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	_, err = in.Invoke("f")
	var trap *wruntime.TrapError
	if !errors.As(err, &trap) || trap.Code != wruntime.TrapIndirectOutOfBounds {
		t.Fatalf("Invoke zero-length indirect call error = %v, want indirect-call OOB trap", err)
	}
}

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
		if AsI32(r[0]) != tc.want {
			t.Errorf("apply(%d,%d,%d) = %d, want %d", tc.which, tc.a, tc.b, AsI32(r[0]), tc.want)
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
		if AsI32(r[0]) != tc.want {
			t.Errorf("apply(%d,10,4) = %d, want %d", tc.which, AsI32(r[0]), tc.want)
		}
	}
}
