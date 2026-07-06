package wagobench

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	wago "github.com/wago-org/wago"
	wasm "github.com/wago-org/wago/src/core/compiler/wasm"
)

// A representative in-memory SQLite workload driven identically on wago and
// wazero: build a table of nRows (recursive-CTE insert), then repeatedly run an
// aggregate SELECT that exercises the VDBE. The SELECT is the timed hot path.
const (
	sqlNRows  = 5000
	sqlSetup  = "CREATE TABLE t(id INTEGER PRIMARY KEY, v INTEGER);"
	sqlInsert = "INSERT INTO t(v) WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i<%d) SELECT (i*1103515245+12345) %% 100000 FROM c;"
	sqlQuery  = "SELECT count(*), sum(v), max(v), avg(v) FROM t WHERE v > 25000;"
)

func sqliteBytes(tb testing.TB) []byte {
	src, err := os.ReadFile("corpus/sqlite3.wasm")
	if err != nil {
		tb.Skip("corpus/sqlite3.wasm not present")
	}
	return src
}

func sqliteMemLimits(m *wasm.Module) (min, max uint32) {
	for _, im := range m.Imports {
		if im.Type.Kind == wasm.ExternMem {
			min = uint32(im.Type.Mem.Limits.Min)
			if im.Type.Mem.Limits.Max != nil {
				max = uint32(*im.Type.Mem.Limits.Max)
			}
		}
	}
	return
}

// ---- wago driver ----------------------------------------------------------

func BenchmarkSqliteQueryWago(b *testing.B) {
	src := sqliteBytes(b)
	c, err := wago.Compile(src)
	if err != nil {
		b.Fatal(err)
	}
	key, _ := c.MemoryImport()
	m, _ := wasm.DecodeModule(src)
	minP, maxP := sqliteMemLimits(m)
	mem, err := wago.NewMemory(minP, maxP)
	if err != nil {
		b.Fatal(err)
	}
	imp := wago.Imports{key: mem}
	for _, fn := range c.Imports {
		imp[fn] = wago.SyncHostFunc(func(wago.HostModule, []uint64, []uint64) {})
	}
	in, err := wago.Instantiate(c, imp)
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	call := func(name string, a ...uint64) uint64 {
		r, err := in.Invoke(name, a...)
		if err != nil {
			b.Fatalf("%s: %v", name, err)
		}
		if len(r) == 0 {
			return 0
		}
		return r[0]
	}
	cstr := func(str string) uint32 {
		bs := append([]byte(str), 0)
		p := uint32(call("malloc", uint64(len(bs))))
		in.Write(p, bs)
		return p
	}
	rd32 := func(p uint32) uint32 { bs, _ := in.Read(p, 4); return binary.LittleEndian.Uint32(bs) }
	in.Invoke("__wasm_call_ctors")
	call("sqlite3_initialize")
	ppDb := uint32(call("malloc", 8))
	if rc := call("sqlite3_open", uint64(cstr(":memory:")), uint64(ppDb)); rc != 0 {
		b.Fatalf("open rc=%d", rc)
	}
	db := uint64(rd32(ppDb))
	execSql := func(sql string) {
		if rc := call("sqlite3_exec", db, uint64(cstr(sql)), 0, 0, 0); rc != 0 {
			e := uint32(call("sqlite3_errmsg", db))
			bs, _ := in.Read(e, 64)
			b.Fatalf("exec %q rc=%d err=%q", sql, rc, string(bs))
		}
	}
	querySql := func(sql string) {
		pp := uint32(call("malloc", 8))
		if rc := call("sqlite3_prepare_v2", db, uint64(cstr(sql)), uint64(uint32(0xFFFFFFFF)), uint64(pp), 0); rc != 0 {
			b.Fatalf("prepare rc=%d", rc)
		}
		stmt := uint64(rd32(pp))
		for call("sqlite3_step", stmt) == 100 {
		}
		call("sqlite3_finalize", stmt)
		call("free", uint64(pp))
	}
	execSql(sqlSetup)
	execSql(fmt.Sprintf(sqlInsert, sqlNRows))
	querySql(sqlQuery) // warm
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		querySql(sqlQuery)
	}
}

// ---- wazero driver (identical workload) -----------------------------------

func watVT(v wasm.ValType) string {
	if v.Kind == wasm.ValNum {
		switch v.Num {
		case wasm.NumI64:
			return "i64"
		case wasm.NumF32:
			return "f32"
		case wasm.NumF64:
			return "f64"
		}
	}
	return "i32"
}

// buildEnvModule generates an "env" wasm (memory export + zero-returning stub
// funcs) matching sqlite's env.* imports, so wazero can link it.
func buildEnvModule(tb testing.TB, m *wasm.Module) []byte {
	var b []byte
	b = append(b, "(module\n"...)
	for _, im := range m.Imports {
		if im.Module != "env" {
			continue
		}
		switch im.Type.Kind {
		case wasm.ExternMem:
			mx := uint64(65536)
			if im.Type.Mem.Limits.Max != nil {
				mx = *im.Type.Mem.Limits.Max
			}
			b = append(b, fmt.Sprintf("  (memory (export \"memory\") %d %d)\n", im.Type.Mem.Limits.Min, mx)...)
		case wasm.ExternFunc:
			comp := m.Types[im.Type.Type.Index].SubTypes[0].Comp
			b = append(b, fmt.Sprintf("  (func (export %q)", im.Name)...)
			for _, p := range comp.Params {
				b = append(b, " (param "+watVT(p)+")"...)
			}
			for _, q := range comp.Results {
				b = append(b, " (result "+watVT(q)+")"...)
			}
			for _, q := range comp.Results {
				b = append(b, " ("+watVT(q)+".const 0)"...)
			}
			b = append(b, ")\n"...)
		}
	}
	b = append(b, ")\n"...)
	dir := tb.TempDir()
	watP, wasmP := dir+"/env.wat", dir+"/env.wasm"
	os.WriteFile(watP, b, 0644)
	if out, err := exec.Command("wat2wasm", watP, "-o", wasmP).CombinedOutput(); err != nil {
		tb.Fatalf("wat2wasm: %v\n%s", err, out)
	}
	bs, _ := os.ReadFile(wasmP)
	return bs
}

func BenchmarkSqliteQueryWazero(b *testing.B) {
	src := sqliteBytes(b)
	m, _ := wasm.DecodeModule(src)
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigCompiler())
	defer r.Close(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)
	if _, err := r.InstantiateWithConfig(ctx, buildEnvModule(b, m), wazero.NewModuleConfig().WithName("env")); err != nil {
		b.Fatal(err)
	}
	mod, err := r.InstantiateWithConfig(ctx, src, wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		b.Fatal(err)
	}
	call := func(name string, a ...uint64) uint64 {
		res, err := mod.ExportedFunction(name).Call(ctx, a...)
		if err != nil {
			b.Fatalf("%s: %v", name, err)
		}
		if len(res) == 0 {
			return 0
		}
		return res[0]
	}
	mem := mod.Memory()
	cstr := func(str string) uint32 {
		bs := append([]byte(str), 0)
		p := uint32(call("malloc", uint64(len(bs))))
		mem.Write(p, bs)
		return p
	}
	rd32 := func(p uint32) uint32 { v, _ := mem.ReadUint32Le(p); return v }
	call("__wasm_call_ctors")
	call("sqlite3_initialize")
	ppDb := uint32(call("malloc", 8))
	call("sqlite3_open", uint64(cstr(":memory:")), uint64(ppDb))
	db := uint64(rd32(ppDb))
	execSql := func(sql string) {
		if rc := call("sqlite3_exec", db, uint64(cstr(sql)), 0, 0, 0); rc != 0 {
			b.Fatalf("exec %q rc=%d", sql, rc)
		}
	}
	querySql := func(sql string) {
		pp := uint32(call("malloc", 8))
		call("sqlite3_prepare_v2", db, uint64(cstr(sql)), uint64(uint32(0xFFFFFFFF)), uint64(pp), 0)
		stmt := uint64(rd32(pp))
		for call("sqlite3_step", stmt) == 100 {
		}
		call("sqlite3_finalize", stmt)
		call("free", uint64(pp))
	}
	execSql(sqlSetup)
	execSql(fmt.Sprintf(sqlInsert, sqlNRows))
	querySql(sqlQuery)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		querySql(sqlQuery)
	}
}
