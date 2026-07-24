package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/wago-org/wago/testutil/wasmtest"
)

// These are the checked-in binary fixtures from wazero's engine integration
// suite at revision 236c2458ed22010150de76c5397eca2c89af3b4f. Keep this
// manifest exact so upstream fixture additions or accidental deletions require
// an explicit applicability decision.
func TestWazeroPortEngineFixtureManifest(t *testing.T) {
	want := []string{
		"eh_br_orphan.wasm",
		"eh_br_own_label.wasm",
		"eh_br_stale_handler.wasm",
		"eh_catch_outside.wasm",
		"eh_cross_callnative.wasm",
		"eh_locals_corrupted.wasm",
		"eh_locals_cross_func.wasm",
		"eh_locals_nested_catch.wasm",
		"eh_locals_nested_nocatch.wasm",
		"eh_pdfium.wasm",
		"eh_throw_ref_null.wasm",
		"global_extend.wasm",
		"host_memory.wasm",
		"huge_call_stack_unwind.wasm",
		"hugestack.wasm",
		"i32_upper_bits.wasm",
		"infinite_loop.wasm",
		"memory.wasm",
		"overflow.wasm",
		"recursive.wasm",
		"reftype_imports.wasm",
		"unreachable.wasm",
		"urem_regalloc.wasm",
	}
	dir := filepath.Join("..", "..", "testdata", "wazero", "engine")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read engine fixtures: %v", err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			got = append(got, entry.Name())
		}
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("engine fixture count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("engine fixture[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWazeroPortFixtureTreeDigest(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "wazero")
	paths := make([]string, 0, 941)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	if len(paths) != 941 {
		t.Fatalf("wazero fixture tree has %d files, want 941", len(paths))
	}
	h := sha256.New()
	artifactCount := 0
	for _, rel := range paths {
		if rel == "README.md" || rel == "LICENSE" {
			continue
		}
		artifactCount++
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	if artifactCount != 939 {
		t.Fatalf("wazero fixture tree has %d upstream artifacts, want 939", artifactCount)
	}
	got := hex.EncodeToString(h.Sum(nil))
	const want = "910700035d51ffc50d380261168120f8d97ef4f0fb42e9c6dfe0824a79b8037a"
	if got != want {
		t.Fatalf("wazero fixture tree digest = %s, want %s", got, want)
	}
}

// Wago does not advertise the exception-handling proposal. These regression
// binaries must therefore fail closed during validation, never reach codegen or
// instantiation, and never be silently dropped from the port.
func TestWazeroPortEngineBehaviorFixtures(t *testing.T) {
	t.Run("overflow", func(t *testing.T) {
		in := instantiateWazeroEngineFixture(t, "overflow.wasm", nil)
		defer in.Close()
		for _, export := range []string{"i32", "i64"} {
			got, err := in.Invoke(export)
			if err != nil || len(got) != 1 || got[0] != 1 {
				t.Fatalf("%s() = %v, %v; want [1]", export, got, err)
			}
		}
	})
	t.Run("global_extend", func(t *testing.T) {
		in := instantiateWazeroEngineFixture(t, "global_extend.wasm", nil)
		defer in.Close()
		got, err := in.Invoke("extend")
		if err != nil || len(got) != 1 || got[0] != math.MaxUint32 {
			t.Fatalf("extend() = %v, %v; want [%#x]", got, err, uint64(math.MaxUint32))
		}
	})
	t.Run("host_memory", func(t *testing.T) {
		var observed []byte
		imports := Imports{"host.store_int": HostFunc(func(m HostModule, params, results []uint64) {
			mem := m.Memory()
			off := int(uint32(params[0]))
			if off < 0 || off+8 > len(mem) {
				results[0] = 1
				return
			}
			binary.LittleEndian.PutUint64(mem[off:], params[1])
			observed = mem
		})}
		in := instantiateWazeroEngineFixture(t, "host_memory.wasm", imports)
		defer in.Close()
		got, err := in.Invoke("store_int", I32(1), math.MaxUint64)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("store_int = %v, %v; want [0]", got, err)
		}
		want := []byte{0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0}
		if len(observed) < len(want) {
			t.Fatalf("host-observed memory length = %d, want at least %d", len(observed), len(want))
		}
		if string(observed[:len(want)]) != string(want) {
			t.Fatalf("host-observed memory = %x, want %x", observed[:len(want)], want)
		}
	})
	t.Run("recursive_entry", func(t *testing.T) {
		var in *Instance
		var nestedErr error
		imports := Imports{"env.host_func": HostFunc(func(_ HostModule, _, _ []uint64) {
			_, nestedErr = in.Invoke("called_by_host_func")
		})}
		in = instantiateWazeroEngineFixture(t, "recursive.wasm", imports)
		defer in.Close()
		if _, err := in.Invoke("main", I32(1)); err != nil || nestedErr != nil {
			t.Fatalf("recursive main = %v, nested = %v", err, nestedErr)
		}
	})
	t.Run("unreachable_host_panic", func(t *testing.T) {
		if !requireStandardGoTestRuntime(t) {
			return
		}
		in := instantiateWazeroEngineFixture(t, "unreachable.wasm", Imports{"host.cause_unreachable": HostFunc(func(_ HostModule, _, _ []uint64) {
			panic(errors.New("panic in host function"))
		})})
		defer in.Close()
		defer func() {
			r := recover()
			if err, ok := r.(error); !ok || err.Error() != "panic in host function" {
				t.Fatalf("panic = %T %v, want exact host error", r, r)
			}
		}()
		_, _ = in.Invoke("main")
	})
	t.Run("reftype_imports", func(t *testing.T) {
		rt := NewRuntime()
		defer rt.Close()
		ref, err := rt.NewExternRef("wazero-reftype")
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wazero", "engine", "reftype_imports.wasm"))
		if err != nil {
			t.Fatal(err)
		}
		mod, err := rt.Compile(data)
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithImports(Imports{"host.externref": HostFunc(func(_ HostModule, params, results []uint64) {
			if len(params) != 1 || params[0] != 0 {
				panic("ref.null externref argument was not null")
			}
			results[0] = ValueExternRef(ref).Bits()
		})}))
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		got, err := in.Invoke("get_externref_by_host")
		if err != nil || len(got) != 1 || got[0] != ValueExternRef(ref).Bits() {
			t.Fatalf("get_externref_by_host = %v, %v; want token %#x", got, err, ValueExternRef(ref).Bits())
		}
		if value, ok := rt.ExternRefValue(ref); !ok || value != "wazero-reftype" {
			t.Fatalf("externref resolution = %#v, %v", value, ok)
		}
	})
	t.Run("memory", func(t *testing.T) {
		in := instantiateWazeroEngineFixture(t, "memory.wasm", nil)
		defer in.Close()
		assert := func(export string, want uint64, args ...uint64) {
			t.Helper()
			got, err := in.Invoke(export, args...)
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("%s%v = %v, %v; want [%d]", export, args, got, err, want)
			}
		}
		assert("size", 0)
		if _, err := in.Invoke("store", I32(2*65536-8)); err == nil {
			t.Fatal("store beyond zero-page memory did not trap")
		}
		assert("grow", 0, I32(1))
		assert("size", 1)
		if _, err := in.Invoke("store", I32(2*65536-8)); err == nil {
			t.Fatal("store beyond one-page memory did not trap")
		}
		assert("grow", 1, I32(1))
		assert("size", 2)
		if _, err := in.Invoke("store", I32(2*65536-8)); err != nil {
			t.Fatalf("store at two-page boundary: %v", err)
		}
	})
}

func instantiateWazeroEngineFixture(t *testing.T, name string, imports Imports) *Instance {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "wazero", "engine", name))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(nil, data)
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatalf("instantiate %s: %v", name, err)
	}
	return in
}

func TestWazeroPortTailCallProposalFailsClosed(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0x12, 0x00, 0x0b}), // return_call 0
		)),
	)
	compiled, err := Compile(nil, mod)
	if compiled != nil {
		_ = compiled.Close()
		t.Fatal("unsupported tail-call module compiled")
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("tail-call compile error = %v, want explicit unsupported rejection", err)
	}
}

func TestWazeroPortTypedFunctionReferenceProposalFailsClosed(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("caller", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd2, 0x00, 0x14, 0x00, 0x0b}), // ref.func 0; call_ref type 0
		)),
	)
	compiled, err := Compile(nil, mod)
	if compiled != nil {
		_ = compiled.Close()
		t.Fatal("unsupported typed-function-reference module compiled")
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("call_ref compile error = %v, want explicit unsupported rejection", err)
	}
}

func TestWazeroPortExceptionHandlingFixturesFailClosed(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "wazero", "engine", "eh_*.wasm"))
	if err != nil {
		t.Fatalf("glob exception fixtures: %v", err)
	}
	if len(paths) != 11 {
		t.Fatalf("exception fixture count = %d, want 11", len(paths))
	}
	for _, path := range paths {
		path := path
		t.Run(strings.TrimSuffix(filepath.Base(path), ".wasm"), func(t *testing.T) {
			wasmBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			compiled, err := Compile(nil, wasmBytes)
			if compiled != nil {
				t.Fatal("unsupported exception-handling module compiled")
			}
			if err == nil || !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("compile error = %v, want explicit unsupported exception rejection", err)
			}
		})
	}
}
