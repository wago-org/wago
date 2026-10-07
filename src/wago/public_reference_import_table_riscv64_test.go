//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceImportTableProducer(t *testing.T, vector, borrowed bool) []byte {
	t.Helper()
	source := rv64ReferenceGlobalModule(vector, borrowed, false, false, 1)
	return rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("reference", 3, 0))))
}

// A reader has no imported global through which to discover the producer.
// Its table must retain the actual descriptor owner as soon as the writer returns.
func TestRV64PublicReferenceImportHostTable(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, sameTable := range []bool{false, true} {
				for _, cleanup := range []string{"clear", "close"} {
					t.Run(fmt.Sprintf("vector%t-artifact%t-same%t-%s", vector, artifact, sameTable, cleanup), func(t *testing.T) {
						rt := NewRuntime()
						defer rt.Close()
						table, err := NewTable(2, 5)
						if err != nil {
							t.Fatal(err)
						}
						defer table.Close()
						pc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-table-producer-vector%t-same%t", vector, sameTable), rv64ReferenceImportTableProducer(t, vector, sameTable), artifact)
						producerImports := NewImports()
						if sameTable {
							producerImports.Table("env", "table", table)
						}
						producer := rv64NativeTokenInstantiate(t, rt, pc, producerImports)
						g, err := producer.ExportedGlobalObject("reference")
						if err != nil {
							t.Fatal(err)
						}
						c := rv64TableStageCompile(t, fmt.Sprintf("ref-import-table-consumer-vector%t", vector), rv64ReferenceImportModule(vector, true), artifact)
						in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g).Table("env", "table", table))
						rc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-reader-vector%t", vector), rv64ReferenceGlobalModule(vector, true, false, true, 1), artifact)
						reader := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Table("env", "table", table))
						producer.Close()
						pc.Close()
						c.Close()
						rc.Close()
						if _, err := in.Invoke("install", 0); err != nil {
							t.Fatal(err)
						}
						args, want := []uint64{0xdeadbeef00000007, 0}, []uint64{7}
						if vector {
							args = []uint64{0x0123456789abcdef, 0xfedcba9876543210, 0}
							want = args[:2]
						}
						importedGlobalInvoke(t, reader, "call", want, args...)
						if table.retained[producer] == nil || table.retained[in] != nil || len(rt.refStore.byToken) != 0 {
							t.Fatal("table did not retain actual unissued descriptor owner")
						}
						in.Close()
						if in.hasPhysicalResources() {
							t.Fatal("destination retained writer instead of actual owner")
						}
						importedGlobalInvoke(t, reader, "call", want, args...)
						if cleanup == "clear" {
							if _, err := reader.Invoke("fill", 0, 2); err != nil {
								t.Fatal(err)
							}
							if producer.hasPhysicalResources() {
								t.Fatal("cleared row retained raw owner")
							}
						}
						reader.Close()
						if err := table.Close(); err != nil {
							t.Fatal("closed host-table attachment cycle", err)
						}
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if err := rt.CloseContext(ctx); err != nil {
							t.Fatal(err)
						}
						if producer.hasPhysicalResources() || reader.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil || rc.codeCache.mem != nil {
							t.Fatal("host-table reference resources remain")
						}
					})
				}
			}
		}
	}
}

func rv64ReferenceImportHostTableParkModule(vector bool) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	park := append(append(wasmtest.Name("env"), wasmtest.Name("park")...), 0, 1)
	table := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 5)
	spin := []byte{1, 1, 0x70, 0x23, 0, 0x21, 0, 0x41, 0, 0x20, 0, 0x26, 0, 0x10, 0, 0x41, 1, 0x20, 0, 0x26, 0, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false), park, table)),
		wasmtest.Section(3, []byte{2, 1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 1), wasmtest.ExportEntry("call", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(spin))), spin...), wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b}))),
	)
}

func TestRV64PublicReferenceImportHostTableParked(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, action := range []string{"resume", "cancel", "close"} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-%s", vector, artifact, action), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					table, err := NewTable(2, 5)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					pc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-table-producer-vector%t-sametrue", vector), rv64ReferenceImportTableProducer(t, vector, true), artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Table("env", "table", table))
					g, err := producer.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64TableStageCompile(t, fmt.Sprintf("ref-import-table-park-vector%t", vector), rv64ReferenceImportHostTableParkModule(vector), artifact)
					entered, release := make(chan struct{}, 1), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					defer unblock()
					park := func() { entered <- struct{}{}; <-release; runtime.GC() }
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g).Table("env", "table", table).Function("env", "park", park))
					rc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-reader-vector%t", vector), rv64ReferenceGlobalModule(vector, true, false, true, 1), artifact)
					reader := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Table("env", "table", table))
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					done := make(chan error, 1)
					go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
					joined := false
					defer func() {
						cancel()
						unblock()
						if !joined {
							<-done
						}
					}()
					select {
					case <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("host-table reference did not park")
					}
					producer.Close()
					pc.Close()
					c.Close()
					rc.Close()
					args, want := []uint64{0xdeadbeef00000007, 0}, []uint64{7}
					if vector {
						args = []uint64{0x0123456789abcdef, 0xfedcba9876543210, 0}
						want = args[:2]
					}
					importedGlobalInvoke(t, reader, "call", want, args...)
					if err := table.Close(); err == nil {
						t.Fatal("parked native storage lease lost")
					}
					if action == "cancel" {
						cancel()
					}
					if action == "close" {
						in.Close()
					}
					if action != "resume" {
						deadline := time.Now().Add(5 * time.Second)
						for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
							runtime.Gosched()
						}
						if atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
							t.Fatal("host-table interruption missing")
						}
					}
					unblock()
					select {
					case err := <-done:
						joined = true
						if action == "resume" && err != nil || action != "resume" && err == nil || action == "cancel" && !errors.Is(err, context.Canceled) {
							t.Fatal("host-table parked result", action, err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("host-table reference did not unwind")
					}
					if action == "resume" {
						if !slices.Equal(table.desc[8:40], table.desc[40:72]) {
							t.Fatal("saved reference not restored after park")
						}
					} else if !slices.Equal(table.desc[40:72], make([]byte, 32)) {
						t.Fatal("interrupted reference store resumed")
					}
					in.Close()
					importedGlobalInvoke(t, reader, "call", want, args...)
					reader.Close()
					if err := table.Close(); err != nil {
						t.Fatal("same-table retirement cycle", err)
					}
					closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
					defer stop()
					if err := rt.CloseContext(closeCtx); err != nil {
						t.Fatal(err)
					}
					if producer.hasPhysicalResources() || in.hasPhysicalResources() || reader.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil || rc.codeCache.mem != nil {
						t.Fatal("parked host-table roots remain")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceImportHostTableBindingGate(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				g, err := rt.NewFuncRefGlobal(NullFuncRef(), false)
				if err != nil {
					t.Fatal(err)
				}
				defer g.Close()
				pc := rv64TableStageCompile(t, "ref-import-table-owned-null", rv64ReferenceGlobalModule(false, false, false, true, 1), artifact)
				producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				destination, err := producer.ExportedTable("table")
				if err != nil {
					t.Fatal(err)
				}
				typ := wasm.I32
				if vector {
					typ = wasm.V128
				}
				tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 5)
				unused := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false), tableImport)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))))
				for _, used := range []bool{false, true} {
					source := unused
					if used {
						source = rv64ReferenceImportModule(vector, true)
					}
					c := rv64TableStageCompile(t, fmt.Sprintf("ref-import-table-binding-vector%t-used%t", vector, used), source, artifact)
					mod, err := rt.Module(c)
					if err != nil {
						t.Fatal(err)
					}
					beforeBase, beforeMem, beforeRefs := c.codeCache.base, offHeapSlicePtr(c.codeCache.mem), c.codeCache.refs
					roots := producer.referenceLifetime().snapshot().ResourceRoots
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "ref", g).Table("env", "table", destination)))
					if in != nil {
						in.Close()
					}
					if in != nil || !errors.Is(err, railshot.ErrUnsupported) || c.codeCache.base != beforeBase || offHeapSlicePtr(c.codeCache.mem) != beforeMem || c.codeCache.refs != beforeRefs || producer.referenceLifetime().snapshot().ResourceRoots != roots || g.owner.importers != 0 {
						t.Fatal("instance-owned destination published", used, err)
					}
					table, err := NewTable(2, 5)
					if err != nil {
						t.Fatal(err)
					}
					accepted, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "ref", g).Table("env", "table", table)))
					mod.Close()
					if err != nil {
						table.Close()
						t.Fatal("host destination rejected", used, err)
					}
					if used {
						importedGlobalInvoke(t, accepted, "isnull", []uint64{1})
					}
					accepted.Close()
					if err := table.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestRV64PublicReferenceImportHostTableFailedRefresh(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			table, err := NewTable(2, 5)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			pc := rv64TableStageCompile(t, "ref-import-table-producer-vectorfalse-samefalse", rv64ReferenceImportTableProducer(t, false, false), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			g, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			c := rv64TableStageCompile(t, "ref-import-table-consumer-vectorfalse", rv64ReferenceImportModule(false, true), artifact)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g).Table("env", "table", table))
			canonical, valid := rv64PublishedDescriptor(producer, readGlobalObject(g, ValFuncRef))
			if !valid {
				t.Fatal("producer descriptor")
			}
			for _, header := range [][2]uint32{{3, 2}, {2, 6}, {2, 1025}} {
				producerRoots := producer.referenceLifetime().snapshot().ResourceRoots
				consumerRoots := in.referenceLifetime().snapshot().ResourceRoots
				var scope rv64NativeTableTargets
				nativeExecutionMu.Lock()
				err := scope.refresh(in)
				if err == nil {
					copy(table.desc[8:40], canonical[:32])
					binary.LittleEndian.PutUint32(table.desc, header[0])
					binary.LittleEndian.PutUint32(table.desc[4:], header[1])
					err = scope.validateReachable()
				}
				nativeExecutionMu.Unlock()
				scope.release()
				balanced := producer.referenceLifetime().snapshot().ResourceRoots == producerRoots && in.referenceLifetime().snapshot().ResourceRoots == consumerRoots && len(table.retained) == 0
				nativeExecutionMu.Lock()
				binary.LittleEndian.PutUint32(table.desc, 2)
				binary.LittleEndian.PutUint32(table.desc[4:], 5)
				clear(table.desc[8:])
				nativeExecutionMu.Unlock()
				if err == nil || !balanced {
					t.Fatal("failed refresh published roots", header, err, balanced)
				}
			}
			in.Close()
			producer.Close()
			if err := table.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64PublicReferenceImportHostTableProxyOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			host := rv64HostTokenOwner(t, rt, ValI32, slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p); runtime.GC() }))
			pc := rv64HostTokenCompile(t, 0, artifact)
			canonicalOwner := rv64NativeTokenInstantiate(t, rt, pc, rv64HostTokenImports(host))
			token, err := canonicalOwner.GlobalValue("copy")
			if err != nil || token.FuncRef().IsNull() {
				t.Fatal("canonical host token", err)
			}
			rawOwner := rv64NativeTokenInstantiate(t, rt, pc, rv64HostTokenImports(host))
			g, err := rawOwner.ExportedGlobalObject("copy")
			if err != nil {
				t.Fatal(err)
			}
			table, err := NewTable(2, 5)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			c := rv64TableStageCompile(t, "ref-import-table-consumer-vectorfalse", rv64ReferenceImportModule(false, true), artifact)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g).Table("env", "table", table))
			rc := rv64TableStageCompile(t, "ref-import-reader-vectorfalse", rv64ReferenceGlobalModule(false, true, false, true, 1), artifact)
			reader := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Table("env", "table", table))
			canonicalOwner.Close()
			rawOwner.Close()
			pc.Close()
			c.Close()
			rc.Close()
			if _, err := in.Invoke("install", 0); err != nil {
				t.Fatal(err)
			}
			in.Close()
			if table.retained[rawOwner] == nil || table.retained[canonicalOwner] != nil || in.hasPhysicalResources() {
				t.Fatal("host table retained token owner instead of raw slot owner")
			}
			importedGlobalInvoke(t, reader, "call", []uint64{7}, 0xdeadbeef00000007, 0)
			if _, err := reader.Invoke("fill", 0, 2); err != nil {
				t.Fatal(err)
			}
			if rawOwner.hasPhysicalResources() || !canonicalOwner.hasPhysicalResources() {
				t.Fatal("raw and token owner release were conflated")
			}
			reader.Close()
			if err := table.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := rt.CloseContext(ctx); err != nil {
				t.Fatal(err)
			}
			if err := host.Close(); err != nil {
				t.Fatal(err)
			}
			if canonicalOwner.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil || rc.codeCache.mem != nil {
				t.Fatal("host table retained final proxy resources")
			}
		})
	}
}
