//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"os"
	"slices"
	"testing"
	"unsafe"
)

func rv64CompactExternBulkModule(vector bool, imported ...bool) []byte {
	return rv64CompactExternBulkModuleWith(vector, len(imported) != 0 && imported[0], false)
}
func rv64CompactExternBulkModuleWith(vector, imported, passive bool, limits ...byte) []byte {
	tableMin, tableMax := byte(4), byte(8)
	if len(limits) >= 2 {
		tableMin, tableMax = limits[0], limits[1]
	}
	types := [][]byte{
		{0x5f, 1, 0x7f, 0},
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.ExternRef, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.ExternRef, wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.ExternRef}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.ExternRef}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, nil),
	}
	bodies := [][]byte{
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 17, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 14, 0, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0xfc, 15, 0, 0x0b},
		{0x20, 0, 0x25, 0, 0x0b},
		{0x20, 0, 0x20, 1, 0x26, 0, 0x0b},
		{0x20, 0, 0xfb, 0, 0, 0xfb, 27, 0x0b},
		{0x20, 0, 0x25, 0, 0xfb, 26, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b},
		{0xfc, 16, 0, 0x0b},
		{0x41, 0, 0xd0, 0x6f, 0x41, 6, 0xfc, 17, 0, 0x0b},
	}
	names := []string{"fill", "copy", "grow", "get", "set", "make", "read", "size", "clear"}
	functionTypes := [][]byte{{1}, {2}, {3}, {4}, {5}, {4}, {6}, {7}, {8}}
	failStart := len(limits) > 2 && limits[2] == 1
	if passive {
		names = append(names, "init", "drop")
		functionTypes = append(functionTypes, []byte{2}, []byte{8})
		bodies = append(bodies, []byte{0x20, 0, 0x20, 1, 0x20, 2, 0xfc, 12, 0, 0, 0x0b}, []byte{0xfc, 13, 0, 0x0b})
	}
	if failStart {
		names = append(names, "fail")
		functionTypes = append(functionTypes, []byte{8})
		bodies = append(bodies, []byte{0, 0x0b})
	}
	code := make([][]byte, len(bodies))
	exports := make([][]byte, len(bodies))
	for i, body := range bodies {
		if vector {
			prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
			body = append(append(prefix, 0x1a), body...)
		}
		code[i] = wasmtest.Code(body)
		exports[i] = wasmtest.ExportEntry(names[i], 0, uint32(i))
	}
	exports = append(exports, wasmtest.ExportEntry("table", 1, 0))
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	var imports [][]byte
	if imported {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x6f, 1, tableMin, tableMax))
	}
	if passive {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("value")...), 3, 0x6f, 0))
	}
	if len(imports) != 0 {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(functionTypes...)))
	if !imported {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x6f, 1, tableMin, tableMax})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if failStart {
		sections = append(sections, wasmtest.Section(8, wasmtest.ULEB(uint32(len(bodies)-1))))
	}
	if passive {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{5, 0x6f, 3, 0x23, 0, 0x0b, 0xd0, 0x6f, 0x0b, 0x23, 0, 0x0b})))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code...)))
	return wasmtest.Module(sections...)
}

func TestRV64CompactExternBulkPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModule(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for artifact, candidate := range []*Compiled{c, loaded} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					in, err := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					invoke := func(name string, args ...uint64) []uint64 {
						t.Helper()
						got, err := in.Invoke(name, args...)
						if err != nil {
							t.Fatal(name, err)
						}
						return got
					}
					token42 := invoke("make", 42)[0]
					invoke("fill", 0, token42, 4)
					if !rt.ReleaseExternRef(ExternRef{token42}) {
						t.Fatal("table shell retained public token instead of compact root")
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					check := func(want []uint64) {
						t.Helper()
						for i, v := range want {
							if got := invoke("read", uint64(i)); len(got) != 1 || got[0] != v {
								t.Fatalf("entry%d=%v want%d", i, got, v)
							}
						}
					}
					check([]uint64{42, 42, 42, 42})
					if _, err := in.Invoke("fill", 3, 0, 2); err == nil {
						t.Fatal("out-of-bounds fill did not trap")
					}
					check([]uint64{42, 42, 42, 42})
					token7 := invoke("make", 7)[0]
					invoke("set", 1, token7)
					invoke("copy", 1, 0, 3)
					check([]uint64{42, 42, 7, 42})
					if _, err := in.Invoke("copy", 0, 5, 0); err == nil {
						t.Fatal("zero copy ignored source bound")
					}
					if _, err := in.Invoke("copy", 0, 4, 1); err == nil {
						t.Fatal("out-of-bounds copy did not trap")
					}
					check([]uint64{42, 42, 7, 42})
					invoke("copy", 4, 4, 0)
					if got := invoke("grow", token7, 5); len(got) != 1 || got[0] != uint64(^uint32(0)) {
						t.Fatal("failed grow", got)
					}
					if got := invoke("size"); !slices.Equal(got, []uint64{4}) {
						t.Fatal("failed growth changed length", got)
					}
					if got := invoke("grow", token7, 2); !slices.Equal(got, []uint64{4}) {
						t.Fatal("growth old length", got)
					}
					if !rt.ReleaseExternRef(ExternRef{token7}) {
						t.Fatal("growth retained public token")
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					check([]uint64{42, 42, 7, 42, 7, 7})
					invoke("fill", 0, 0, 6)
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					value, _ := compactExternCollectors.Load(in.gc)
					state := value
					if len(state.shells) != 0 {
						t.Fatal("cleared table retained shells", len(state.shells))
					}
					foreign, err := rt.NewExternRef([]int{1, 2, 3})
					if err != nil {
						t.Fatal(err)
					}
					invoke("fill", 0, foreign.token, 6)
					if rt.ReleaseExternRef(foreign) {
						t.Fatal("table lost foreign pins")
					}
					// A corrupt second source must undo the first prepared pin and leave
					// all destination cells unchanged.
					descriptor := gcRetainedTableDescriptor(in, 0)
					previous := binary.LittleEndian.Uint64(descriptor[16:24])
					binary.LittleEndian.PutUint64(descriptor[16:24], ^uint64(0))
					_, copyErr := in.Invoke("copy", 2, 0, 2)
					binary.LittleEndian.PutUint64(descriptor[16:24], previous)
					if copyErr == nil {
						t.Fatal("malformed second source was accepted")
					}
					for _, index := range []uint64{2, 3} {
						if got := invoke("get", index); len(got) != 1 || got[0] != foreign.token {
							t.Fatal("trapping copy changed destination", index, got)
						}
					}
					invoke("copy", 1, 0, 5)
					invoke("fill", 0, 0, 3)
					if rt.ReleaseExternRef(foreign) {
						t.Fatal("partial clear dropped all foreign pins")
					}
					invoke("fill", 3, 0, 3)
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(foreign) {
						t.Fatal("clear retained foreign pins")
					}
					fn, err := in.WasmFunc("clear")
					if err != nil {
						t.Fatal(err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					if allocs := testing.AllocsPerRun(1000, func() {
						if _, err := session.Invoke0(); err != nil {
							panic(err)
						}
					}); allocs != 0 {
						t.Fatal("warm clear fill Go allocations", allocs)
					}
					session.Close()
					warm, err := rt.NewExternRef("warm foreign fill")
					if err != nil {
						t.Fatal(err)
					}
					fn, err = in.WasmFunc("fill")
					if err != nil {
						t.Fatal(err)
					}
					fillSession, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					allocs := testing.AllocsPerRun(1000, func() {
						if _, err := fillSession.Invoke3(0, warm.token, 6); err != nil {
							panic(err)
						}
					})
					fillSession.Close()
					if allocs != 0 {
						t.Fatal("warm foreign fill Go allocations", allocs)
					}
					invoke("clear")
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(warm) {
						t.Fatal("warm fill retained foreign pins after clear")
					}
					t.Logf("collector sidecar=%d Go bytes; persistent cell=%d bytes; owner ledger=%d bytes; scratch capacity=%d", unsafe.Sizeof(compactExternCollector{}), unsafe.Sizeof(compactExternPersistentCell{}), unsafe.Sizeof(compactExternPersistentOwner{}), cap(state.bulk))
				})
			}
		}
	}
}

func TestRV64CompactExternImportedBulkPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		source, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModule(vector))
		if err != nil {
			t.Fatal(err)
		}
		defer source.Close()
		imported, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModule(vector, true))
		if err != nil {
			t.Fatal(err)
		}
		defer imported.Close()
		loadedSource, loadedImported := publicArtifactRoundTrip(t, source), publicArtifactRoundTrip(t, imported)
		defer loadedSource.Close()
		defer loadedImported.Close()
		for artifact, pair := range [][2]*Compiled{{source, imported}, {loadedSource, loadedImported}} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					pm, err := rt.Module(pair[0])
					if err != nil {
						t.Fatal(err)
					}
					defer pm.Close()
					cm, err := rt.Module(pair[1])
					if err != nil {
						t.Fatal(err)
					}
					defer cm.Close()
					cfg := GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}
					producer, err := rt.Instantiate(context.Background(), pm, WithGC(cfg))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					table, err := producer.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					consumer, err := rt.Instantiate(context.Background(), cm, WithImports(NewImports().Table("env", "table", table)), WithGC(cfg))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					if producer.gc != consumer.gc {
						t.Fatal("imported extern storage did not retain its collector domain")
					}
					token, err := producer.Invoke("make", 42)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = consumer.Invoke("fill", 0, token[0], 4); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(ExternRef{token[0]}) {
						t.Fatal("converted public token did not release")
					}
					if err = producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err = table.Close(); err != nil {
						t.Fatal(err)
					}
					if err = consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					for i := uint64(0); i < 4; i++ {
						got, err := consumer.Invoke("read", i)
						if err != nil || len(got) != 1 || got[0] != 42 {
							t.Fatalf("closed physical owner entry %d=%v,%v", i, got, err)
						}
					}
					if _, err = consumer.Invoke("copy", 1, 0, 3); err != nil {
						t.Fatal(err)
					}
					foreign, err := rt.NewExternRef("retained by imported storage")
					if err != nil {
						t.Fatal(err)
					}
					if _, err = consumer.Invoke("fill", 0, foreign.token, 4); err != nil {
						t.Fatal(err)
					}
					if rt.ReleaseExternRef(foreign) {
						t.Fatal("imported cells lost their foreign pins")
					}
					if _, err = consumer.Invoke("fill", 0, 0, 4); err != nil {
						t.Fatal(err)
					}
					if err := consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(foreign) {
						t.Fatal("imported cells did not release cleared foreign pins")
					}
					if err = consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					state, _ := compactExternCollectors.Load(consumer.gc)
					if len(state.shells) != 0 {
						t.Fatal("cleared imported table retained conversion shells")
					}
					if err = consumer.Close(); err != nil {
						t.Fatal(err)
					}
					producer.lifeMu.Lock()
					closed := producer.resourcesClosed
					producer.lifeMu.Unlock()
					if !closed {
						t.Fatal("last imported binding retained the closed physical producer")
					}
				})
			}
		}
	}
}

func TestRV64CompactExternPassiveBulkPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModuleWith(vector, false, true))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for artifact, candidate := range []*Compiled{c, loaded} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					foreign, err := rt.NewExternRef("passive segment")
					if err != nil {
						t.Fatal(err)
					}
					seed, err := rt.NewExternRefGlobal(foreign, false)
					if err != nil {
						t.Fatal(err)
					}
					defer seed.Close()
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", seed)), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					invoke := func(name string, args ...uint64) []uint64 {
						t.Helper()
						got, err := in.Invoke(name, args...)
						if err != nil {
							t.Fatal(name, err)
						}
						return got
					}
					check := func(want []uint64) {
						t.Helper()
						for i, w := range want {
							if got := invoke("get", uint64(i)); len(got) != 1 || got[0] != w {
								t.Fatalf("entry%d=%v want%d", i, got, w)
							}
						}
					}
					if rt.ReleaseExternRef(foreign) {
						t.Fatal("passive entries lost foreign pins")
					}
					invoke("init", 0, 0, 3)
					check([]uint64{foreign.token, 0, foreign.token, 0})
					if _, err := in.Invoke("init", 0, 2, 2); err == nil {
						t.Fatal("out-of-bounds source did not trap")
					}
					check([]uint64{foreign.token, 0, foreign.token, 0})
					if _, err := in.Invoke("init", 3, 0, 2); err == nil {
						t.Fatal("out-of-bounds destination did not trap")
					}
					check([]uint64{foreign.token, 0, foreign.token, 0})
					invoke("drop")
					invoke("drop")
					invoke("init", 4, 0, 0)
					if _, err := in.Invoke("init", 0, 1, 0); err == nil {
						t.Fatal("dropped segment ignored zero-length source bound")
					}
					if _, err := in.Invoke("init", 0, 0, 1); err == nil {
						t.Fatal("dropped segment accepted nonempty init")
					}
					check([]uint64{foreign.token, 0, foreign.token, 0})
					invoke("fill", 0, 0, 4)
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if err := seed.Close(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(foreign) {
						t.Fatal("closed passive/import/table cells retained foreign pins")
					}
				})
			}
		}
	}
}

func TestRV64CompactExternBulkScratchPrivate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		c, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModuleWith(vector, false, false, 12, 16))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		loaded := publicArtifactRoundTrip(t, c)
		defer loaded.Close()
		for artifact, candidate := range []*Compiled{c, loaded} {
			for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
				t.Run(fmt.Sprintf("vector%t/artifact%t/profile%d", vector, artifact != 0, profile), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					mod, err := rt.Module(candidate)
					if err != nil {
						t.Fatal(err)
					}
					defer mod.Close()
					in, err := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, VerifyAfterCollect: true}))
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					foreign, err := rt.NewExternRef("bulk scratch")
					if err != nil {
						t.Fatal(err)
					}
					if _, err = in.Invoke("fill", 0, foreign.token, 12); err != nil {
						t.Fatal(err)
					}
					if _, err = in.Invoke("copy", 0, 0, 12); err != nil {
						t.Fatal(err)
					}
					fn, err := in.WasmFunc("copy")
					if err != nil {
						t.Fatal(err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					allocs := testing.AllocsPerRun(1000, func() {
						if _, err := session.Invoke3(0, 0, 12); err != nil {
							panic(err)
						}
					})
					session.Close()
					if allocs != 0 {
						t.Fatal("warm copy Go allocations", allocs)
					}
					state, _ := compactExternCollectors.Load(in.gc)
					if cap(state.bulk) != 12 {
						t.Fatal("copy scratch capacity", cap(state.bulk))
					}
					for _, v := range state.bulk[:cap(state.bulk)] {
						if v != (compactExternPreparedWord{}) {
							t.Fatal("copy scratch retained a prepared root/pin")
						}
					}
					if _, err := in.Invoke("fill", 0, 0, 12); err != nil {
						t.Fatal(err)
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if !rt.ReleaseExternRef(foreign) {
						t.Fatal("bulk scratch retained a foreign pin")
					}
					t.Logf("copy scratch=%d Go bytes, reused for 1000 calls", cap(state.bulk)*int(unsafe.Sizeof(compactExternPreparedWord{})))
				})
			}
		}
	}
}

func TestRV64CompactExternConstructorRollbackPrivate(t *testing.T) {
	c, err := NewRuntimeConfig().Compile(rv64CompactExternBulkModuleWith(false, false, true, 4, 8, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for artifact, candidate := range []*Compiled{c, loaded} {
		for _, profile := range []gc.Profile{gc.ProfileThroughput, gc.ProfileTiny} {
			t.Run(fmt.Sprintf("artifact%t/profile%d", artifact != 0, profile), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				mod, err := rt.Module(candidate)
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				foreign, err := rt.NewExternRef("failed constructor")
				if err != nil {
					t.Fatal(err)
				}
				seed, err := rt.NewExternRefGlobal(foreign, false)
				if err != nil {
					t.Fatal(err)
				}
				defer seed.Close()
				if in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "value", seed)), WithGC(GCConfig{Profile: profile, CollectEveryAlloc: true, VerifyAfterCollect: true})); err == nil {
					in.Close()
					t.Fatal("unreachable start did not trap")
				}
				if err := seed.Close(); err != nil {
					t.Fatal(err)
				}
				if !rt.ReleaseExternRef(foreign) {
					t.Fatal("failed constructor retained extern pins")
				}
			})
		}
	}
}

func TestRV64CompactExternPreparePrivate(t *testing.T) {
	dir := os.Getenv("WAGO_RV64_COMPACT_EXTERN_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("private artifact producer")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, vector := range []bool{false, true} {
		for _, fixture := range []struct {
			name   string
			source []byte
		}{{"bulk", rv64CompactExternBulkModule(vector)}, {"passive", rv64CompactExternBulkModuleWith(vector, false, true)}, {"constant", rv64CompactExternConstantModule(vector)}, {"aggregate-struct", rv64CompactExternAggregateSource(vector, false)}, {"aggregate-array", rv64CompactExternAggregateSource(vector, true)}, {"exception", rv64CompactExternExceptionSource(vector)}, {"host-bulk", rv64CompactExternBulkModule(vector, true)}, {"host-cycle", rv64CompactExternHostCycleSource(vector)}, {"constant-struct", rv64CompactExternAggregateConstantSource(vector, false)}, {"constant-array", rv64CompactExternAggregateConstantSource(vector, true)}, {"host-global", rv64CompactExternHostGlobalSource(vector)}, {"host-global-cycle", rv64CompactExternHostGlobalCycleSource(vector)}, {"plain-reentry", rv64CompactExternPlainReentrySource(vector)}, {"function-signature", rv64FunctionSignatureFrameSource(vector)}, {"function-producer", rv64FunctionSignatureProducerSource()}} {
			c, err := NewRuntimeConfig().Compile(fixture.source)
			if err != nil {
				t.Fatal(err)
			}
			data, err := c.MarshalBinary()
			c.Close()
			if err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("%s-%t.wago", fixture.name, vector)
			if err := os.WriteFile(dir+"/"+name, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
