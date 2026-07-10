//go:build arm64 && (darwin || linux)

package wago

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func arm64TableWasm(t *testing.T, wat string) []byte {
	t.Helper()
	w2w, err := exec.LookPath("wat2wasm")
	if err != nil {
		t.Skip("wat2wasm (wabt) not on PATH")
	}
	dir := t.TempDir()
	src, out := filepath.Join(dir, "m.wat"), filepath.Join(dir, "m.wasm")
	if err := os.WriteFile(src, []byte(wat), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(w2w, src, "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("wat2wasm: %v\n%s", err, output)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestARM64IndexedTableOperations(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(arm64TableWasm(t, `(module
        (type $get (func (param i32) (result externref)))
        (table $ext0 3 3 externref)
        (table $fun1 1 1 funcref)
        (table $ext2 1 4 externref)
        (elem $nulls externref (ref.null extern) (ref.null extern))
        (func (export "size2") (result i32) (table.size $ext2))
        (func (export "get2") (type $get) (table.get $ext2 (local.get 0)))
        (func (export "set2") (param i32 externref) (table.set $ext2 (local.get 0) (local.get 1)))
        (func (export "grow2") (param externref i32) (result i32)
          (table.grow $ext2 (local.get 0) (local.get 1)))
        (func (export "fill2") (param i32 externref i32)
          (table.fill $ext2 (local.get 0) (local.get 1) (local.get 2)))
        (func (export "copy0to2") (param i32 i32 i32)
          (table.copy $ext2 $ext0 (local.get 0) (local.get 1) (local.get 2)))
        (func (export "init2") (param i32 i32 i32)
          (table.init $ext2 $nulls (local.get 0) (local.get 1) (local.get 2)))
        (func (export "fun-null") (result i32)
          (ref.is_null (table.get $fun1 (i32.const 0))))
      )`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	ref := issueExternref(t, rt, "indexed-table")
	if _, err := in.Call(context.Background(), "set2", ValueI32(0), ValueExternRef(ref)); err != nil {
		t.Fatalf("set2: %v", err)
	}
	got, err := in.Call(context.Background(), "get2", ValueI32(0))
	if err != nil || len(got) != 1 || got[0].ExternRef() != ref {
		t.Fatalf("get2 = %v, %v; want %v", got, err, ref)
	}
	got, err = in.Call(context.Background(), "grow2", ValueExternRef(ref), ValueI32(2))
	if err != nil || got[0].I32() != 1 {
		t.Fatalf("grow2 = %v, %v; want old size 1", got, err)
	}
	if _, err := in.Call(context.Background(), "fill2", ValueI32(1), ValueExternRef(ref), ValueI32(2)); err != nil {
		t.Fatalf("fill2: %v", err)
	}
	got, err = in.Call(context.Background(), "size2")
	if err != nil || got[0].I32() != 3 {
		t.Fatalf("size2 = %v, %v; want 3", got, err)
	}
	if _, err := in.Call(context.Background(), "copy0to2", ValueI32(0), ValueI32(0), ValueI32(2)); err != nil {
		t.Fatalf("copy0to2: %v", err)
	}
	got, err = in.Call(context.Background(), "get2", ValueI32(0))
	if err != nil || !got[0].ExternRef().IsNull() {
		t.Fatalf("get2 after copy = %v, %v; want null", got, err)
	}
	if _, err := in.Call(context.Background(), "init2", ValueI32(0), ValueI32(0), ValueI32(2)); err != nil {
		t.Fatalf("init2: %v", err)
	}
	if _, err := in.Call(context.Background(), "set2", ValueI32(2), ValueExternRef(ref)); err != nil {
		t.Fatalf("set2 before trapping fill: %v", err)
	}
	if _, err := in.Call(context.Background(), "fill2", ValueI32(2), ValueExternRef(NullExternRef()), ValueI32(2)); err == nil {
		t.Fatal("out-of-bounds fill2 unexpectedly succeeded")
	}
	got, err = in.Call(context.Background(), "get2", ValueI32(2))
	if err != nil || got[0].ExternRef() != ref {
		t.Fatalf("trapping fill mutated table: %v, %v; want %v", got, err, ref)
	}
	got, err = in.Call(context.Background(), "fun-null")
	if err != nil || got[0].I32() != 1 {
		t.Fatalf("fun-null = %v, %v; want 1", got, err)
	}
}
