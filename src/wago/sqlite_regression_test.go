//go:build linux && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"os"
	"testing"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestSyncSQLiteQuery is a codegen regression test: it runs a real in-memory
// SQLite query (the corpus sqlite3.wasm, an emscripten build that imports
// env.memory) end to end and checks the result. It guards a register-allocator
// bug where pinning a hot local to RDI/RSI in a memory-touching, multi-arg-call
// function silently corrupted the local's value — SQLite's tokenizer then misread
// every keyword ("near \"CREATE\": syntax error"). See gpPinPool in the amd64
// backend. Skips if the corpus binary isn't present.
func TestSyncSQLiteQuery(t *testing.T) {
	src, err := os.ReadFile("../../bench/corpus/sqlite3.wasm")
	if err != nil {
		t.Skip("bench/corpus/sqlite3.wasm not present")
	}
	c, err := Compile(nil, src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	key, ok := c.MemoryImport()
	if !ok {
		t.Fatalf("expected sqlite3.wasm to import a memory")
	}
	mod, err := wasm.DecodeModule(src)
	if err != nil {
		t.Fatal(err)
	}
	var minP, maxP uint32
	for _, im := range mod.Imports {
		if im.Type.Kind == wasm.ExternMem {
			minP = uint32(im.Type.Mem.Limits.Min)
			if im.Type.Mem.Limits.Max != nil {
				maxP = uint32(*im.Type.Mem.Limits.Max)
			}
		}
	}
	mem, err := NewMemory(minP, maxP)
	if err != nil {
		t.Fatal(err)
	}
	imp := Imports{key: mem}
	for _, fn := range c.Imports { // no-op stubs for the emscripten env.* / wasi imports
		imp[fn] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: imp})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	call := func(name string, a ...uint64) uint64 {
		r, err := in.Invoke(name, a...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(r) == 0 {
			return 0
		}
		return r[0]
	}
	cstr := func(s string) uint32 {
		b := append([]byte(s), 0)
		p := uint32(call("malloc", uint64(len(b))))
		if !in.Write(p, b) {
			t.Fatalf("write %q to memory", s)
		}
		return p
	}
	rd32 := func(p uint32) uint32 {
		b, ok := in.Read(p, 4)
		if !ok {
			t.Fatalf("read @%d", p)
		}
		return binary.LittleEndian.Uint32(b)
	}

	in.Invoke("__wasm_call_ctors")
	call("sqlite3_initialize")

	ppDb := uint32(call("malloc", 8))
	if rc := call("sqlite3_open", uint64(cstr(":memory:")), uint64(ppDb)); rc != 0 {
		t.Fatalf("sqlite3_open rc=%d", rc)
	}
	db := uint64(rd32(ppDb))

	rc := call("sqlite3_exec", db, uint64(cstr("CREATE TABLE t(x INTEGER); INSERT INTO t VALUES (42),(58),(900);")), 0, 0, 0)
	if rc != 0 {
		e := uint32(call("sqlite3_errmsg", db))
		b, _ := in.Read(e, 48)
		t.Fatalf("sqlite3_exec(CREATE/INSERT) rc=%d errmsg=%q — tokenizer/codegen regression", rc, string(b))
	}

	ppStmt := uint32(call("malloc", 8))
	if rc := call("sqlite3_prepare_v2", db, uint64(cstr("SELECT sum(x) FROM t")), uint64(uint32(0xFFFFFFFF)), uint64(ppStmt), 0); rc != 0 {
		t.Fatalf("sqlite3_prepare_v2 rc=%d", rc)
	}
	stmt := uint64(rd32(ppStmt))
	call("sqlite3_step", stmt)
	sum := int32(uint32(call("sqlite3_column_int", stmt, 0)))
	call("sqlite3_finalize", stmt)
	if sum != 1000 {
		t.Fatalf("SELECT sum(x) = %d, want 1000", sum)
	}
}
