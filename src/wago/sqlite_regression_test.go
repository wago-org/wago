//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"encoding/binary"
	"math"
	"os"
	"testing"

	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

const (
	sqliteSetup  = "CREATE TABLE t(id INTEGER PRIMARY KEY, v INTEGER);"
	sqliteInsert = "INSERT INTO t(v) WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i<5000) SELECT (i*1103515245+12345) % 100000 FROM c;"
	sqliteQuery  = "SELECT count(*), sum(v), max(v), avg(v) FROM t WHERE v > 25000;"
)

// TestSyncSQLiteQuery is an end-to-end codegen regression for the real SQLite
// corpus binary. It deliberately drives the benchmark's recursive-CTE setup and
// aggregate query, rather than merely checking that a statement prepares. The
// fixed goldens were independently evaluated with SQLite 3.46.0:
// count=3755, sum=234590605, max=99995, avg=62474.19573901465.
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
		t.Fatal("sqlite3.wasm has no imported memory")
	}
	mod, err := wasm.DecodeModule(src)
	if err != nil {
		t.Fatal(err)
	}
	var minPages, maxPages uint32
	for _, im := range mod.Imports {
		if im.Type.Kind != wasm.ExternMem {
			continue
		}
		minPages = uint32(im.Type.Mem.Limits.Min)
		if im.Type.Mem.Limits.Max != nil {
			maxPages = uint32(*im.Type.Mem.Limits.Max)
		}
	}
	mem, err := NewMemory(minPages, maxPages)
	if err != nil {
		t.Fatal(err)
	}
	imports := Imports{key: mem}
	for _, name := range c.Imports {
		imports[name] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	call := func(name string, args ...uint64) uint64 {
		t.Helper()
		results, err := in.Invoke(name, args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(results) == 0 {
			return 0
		}
		return results[0]
	}
	read32 := func(ptr uint32) uint32 {
		t.Helper()
		b, ok := in.Read(ptr, 4)
		if !ok {
			t.Fatalf("read i32 at %#x", ptr)
		}
		return binary.LittleEndian.Uint32(b)
	}
	cstr := func(s string) uint32 {
		t.Helper()
		b := append([]byte(s), 0)
		p := uint32(call("malloc", uint64(len(b))))
		if p == 0 || !in.Write(p, b) {
			t.Fatalf("allocate/write SQL string %q", s)
		}
		return p
	}
	errmsg := func(db uint64) string {
		if db == 0 {
			return "no database handle"
		}
		p := uint32(call("sqlite3_errmsg", db))
		if p == 0 {
			return "sqlite3_errmsg returned null"
		}
		b, ok := in.Read(p, 256)
		if !ok {
			return "sqlite3_errmsg points outside memory"
		}
		for i, c := range b {
			if c == 0 {
				return string(b[:i])
			}
		}
		return string(b)
	}
	checkRC := func(op string, rc, db uint64) {
		t.Helper()
		if rc != 0 {
			t.Fatalf("%s rc=%d: %s", op, rc, errmsg(db))
		}
	}

	call("__wasm_call_ctors")
	checkRC("sqlite3_initialize", call("sqlite3_initialize"), 0)
	ppDB := uint32(call("malloc", 8))
	if ppDB == 0 {
		t.Fatal("malloc database pointer")
	}
	if rc := call("sqlite3_open", uint64(cstr(":memory:")), uint64(ppDB)); rc != 0 {
		t.Fatalf("sqlite3_open rc=%d: %s", rc, errmsg(uint64(read32(ppDB))))
	}
	db := uint64(read32(ppDB))
	if db == 0 {
		t.Fatal("sqlite3_open succeeded with a null database handle")
	}
	defer func() {
		if rc := call("sqlite3_close_v2", db); rc != 0 {
			t.Errorf("sqlite3_close_v2 rc=%d: %s", rc, errmsg(db))
		}
	}()
	checkRC("sqlite3_exec setup", call("sqlite3_exec", db, uint64(cstr(sqliteSetup)), 0, 0, 0), db)
	checkRC("sqlite3_exec recursive insert", call("sqlite3_exec", db, uint64(cstr(sqliteInsert)), 0, 0, 0), db)

	ppStmt := uint32(call("malloc", 8))
	if ppStmt == 0 {
		t.Fatal("malloc statement pointer")
	}
	checkRC("sqlite3_prepare_v2", call("sqlite3_prepare_v2", db, uint64(cstr(sqliteQuery)), uint64(uint32(0xffffffff)), uint64(ppStmt), 0), db)
	stmt := uint64(read32(ppStmt))
	if stmt == 0 {
		t.Fatal("sqlite3_prepare_v2 succeeded with a null statement")
	}
	if rc := call("sqlite3_step", stmt); rc != 100 { // SQLITE_ROW
		t.Fatalf("sqlite3_step first rc=%d: %s", rc, errmsg(db))
	}
	if got := call("sqlite3_column_int64", stmt, 0); got != 3755 {
		t.Fatalf("count=%d, want 3755", got)
	}
	if got := call("sqlite3_column_int64", stmt, 1); got != 234590605 {
		t.Fatalf("sum=%d, want 234590605", got)
	}
	if got := call("sqlite3_column_int64", stmt, 2); got != 99995 {
		t.Fatalf("max=%d, want 99995", got)
	}
	if got := math.Float64frombits(call("sqlite3_column_double", stmt, 3)); got != 62474.19573901465 {
		t.Fatalf("avg=%.17g, want %.17g", got, 62474.19573901465)
	}
	if rc := call("sqlite3_step", stmt); rc != 101 { // SQLITE_DONE
		t.Fatalf("sqlite3_step done rc=%d: %s", rc, errmsg(db))
	}
	checkRC("sqlite3_finalize", call("sqlite3_finalize", stmt), db)
}
