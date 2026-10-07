//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64HostTokenCompile(t *testing.T, index int, artifact bool, vectors ...bool) *Compiled {
	t.Helper()
	typ := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[index]
	source := rv64ReplaceTableStageSection(t, rv64NativeTokenModule(false, false, false, false), 1, wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))))
	vector := len(vectors) > 0 && vectors[0]
	if vector {
		source = rv64ReplaceTableStageSection(t, source, 1, wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}))))
		source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{2, 0, 1}))
		body := append([]byte{0xfd, 12}, make([]byte, 16)...)
		body = append(body, 0x0b)
		source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}), wasmtest.Code(body))))
		source = rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("left", 3, 0), wasmtest.ExportEntry("right", 3, 1), wasmtest.ExportEntry("copy", 3, 255), wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("vector", 0, 3))))
	}
	return rv64TableStageCompile(t, fmt.Sprintf("host-token-scalar%d-vector%t", index, vector), source, artifact)
}

func rv64HostTokenOwner(t *testing.T, rt *Runtime, typ ValType, fn slotHostFunc) *HostFuncRef {
	t.Helper()
	h, err := rt.NewHostFuncRef(fn, FuncSig{Params: []ValType{typ}, Results: []ValType{typ}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}
func rv64HostTokenImports(h *HostFuncRef) *Imports {
	return NewImports().Function("env", "left", h).Function("env", "right", h)
}

func TestRV64PublicHostReferenceTokens(t *testing.T) {
	for index, typ := range []ValType{ValI32, ValI64, ValF32, ValF64} {
		for _, vector := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("scalar%d-vector%t-artifact%t", index, vector, artifact), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					c := rv64HostTokenCompile(t, index, artifact, vector)
					calls := 0
					fn := slotHostFunc(func(_ HostModule, p, r []uint64) { calls++; copy(r, p); runtime.GC() })
					owner := rv64HostTokenOwner(t, rt, typ, fn)
					other := rv64HostTokenOwner(t, rt, typ, fn)
					first := rv64NativeTokenInstantiate(t, rt, c, rv64HostTokenImports(owner))
					alias := rv64NativeTokenInstantiate(t, rt, c, rv64HostTokenImports(owner))
					distinct := rv64NativeTokenInstantiate(t, rt, c, rv64HostTokenImports(other))
					c.Close()
					token, err := first.GlobalValue("left")
					if err != nil || token.FuncRef().IsNull() {
						t.Fatal("owned host token", token, err)
					}
					for _, in := range []*Instance{first, alias} {
						for _, name := range []string{"left", "right", "copy"} {
							got, err := in.GlobalValue(name)
							if err != nil || got != token {
								t.Fatal("host alias/copy identity", got, err)
							}
						}
						if !in.FuncRefMatchesFunction(token.FuncRef(), 0) || !in.FuncRefMatchesFunction(token.FuncRef(), 1) || in.FuncRefMatchesFunction(token.FuncRef(), 2) {
							t.Fatal("host semantic identity")
						}
					}
					different, err := distinct.GlobalValue("copy")
					if err != nil || different == token || distinct.FuncRefMatchesFunction(token.FuncRef(), 0) || alias.FuncRefMatchesFunction(different.FuncRef(), 0) {
						t.Fatal("distinct host owners sharing callback/code", different, err)
					}
					descriptor := uint64(offHeapSlicePtr(first.funcRefDescs)) + coreruntime.FuncRefDescBytes
					if n := testing.AllocsPerRun(20, func() {
						if _, err := rt.refStore.issue(first, descriptor); err != nil {
							t.Fatal(err)
						}
					}); n != 0 {
						t.Fatal("cached host issuance allocated", n)
					}
					if n := testing.AllocsPerRun(20, func() {
						if !alias.FuncRefMatchesFunction(token.FuncRef(), 1) {
							t.Fatal("host alias identity")
						}
					}); n != 0 {
						t.Fatal("host matching allocated", n)
					}
					held, err := rt.NewFuncRefGlobal(token.FuncRef(), false)
					if err != nil {
						t.Fatal(err)
					}
					defer held.Close()
					if err := owner.Close(); err == nil {
						t.Fatal("host owner closed with callable importers")
					}
					first.Close()
					runtime.GC()
					if !first.hasPhysicalResources() || len(first.thunkMem) == 0 {
						t.Fatal("canonical host importer lost token root")
					}
					args := []uint64{0xfeedface80000001, 0xfedcba9876543210, 0xdeadbeef7fc12345, 0xfff8123456789abc}
					want := args[index]
					if typ == ValI32 || typ == ValF32 {
						want = uint64(uint32(want))
					}
					importedGlobalInvoke(t, alias, "call", []uint64{want}, args[index])
					if vector {
						importedGlobalInvoke(t, alias, "vector", []uint64{0, 0})
					}
					if calls != 1 {
						t.Fatal("retired canonical host callback", calls)
					}
					if got, err := alias.GlobalValue("copy"); err != nil || got != token {
						t.Fatal("retired host canonical identity", got, err)
					}
					alias.Close()
					distinct.Close()
					if err := owner.Close(); err == nil {
						t.Fatal("host closed while token/store live")
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := rt.CloseContext(ctx); err != nil {
						t.Fatal(err)
					}
					if err := owner.Close(); err != nil {
						t.Fatal(err)
					}
					if err := other.Close(); err != nil {
						t.Fatal(err)
					}
					if got, err := held.GetValue(); err != nil || got != token {
						t.Fatal("host-held token after Runtime/owner close", got, err)
					}
					held.Close()
					if first.hasPhysicalResources() || alias.hasPhysicalResources() || distinct.hasPhysicalResources() || c.codeCache.mem != nil || owner.importers != 0 || owner.tokenLive || owner.source != nil || owner.fn != nil {
						t.Fatal("final owned host token resources")
					}
				})
			}
		}
	}
}

func TestRV64PublicHostReferenceTokenCandidateRetirement(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			c := rv64HostTokenCompile(t, 0, artifact)
			h := rv64HostTokenOwner(t, rt, ValI32, slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p) }))
			first := rv64NativeTokenInstantiate(t, rt, c, rv64HostTokenImports(h))
			second := rv64NativeTokenInstantiate(t, rt, c, rv64HostTokenImports(h))
			descriptor := uint64(offHeapSlicePtr(first.funcRefDescs)) + coreruntime.FuncRefDescBytes
			if _, _, ok := rt.refStore.descriptorFuncrefExactType(first, descriptor); !ok {
				t.Fatal("candidate type inspection")
			}
			if h.source != first || h.tokenLive || len(rt.refStore.byToken) != 0 {
				t.Fatal("type inspection issued a token/root")
			}
			first.Close()
			if first.hasPhysicalResources() {
				t.Fatal("unissued candidate retained")
			}
			token, err := second.GlobalValue("copy")
			if err != nil || token.FuncRef().IsNull() || h.source != second || !h.tokenLive {
				t.Fatal("retired candidate blocked live token issuance", token, err)
			}
			if !second.FuncRefMatchesFunction(token.FuncRef(), 0) {
				t.Fatal("recovered candidate identity")
			}
		})
	}
}

func TestRV64PublicHostReferenceTokenAuthentication(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			c := rv64HostTokenCompile(t, 0, artifact)
			h := rv64HostTokenOwner(t, rt, ValI32, slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p) }))
			in := rv64NativeTokenInstantiate(t, rt, c, rv64HostTokenImports(h))
			alias := rv64NativeTokenInstantiate(t, rt, c, rv64HostTokenImports(h))
			row := in.funcRefDescs[coreruntime.FuncRefDescBytes : 2*coreruntime.FuncRefDescBytes]
			original := slices.Clone(row)
			var token Value
			for _, cached := range []bool{false, true} {
				if cached {
					var err error
					token, err = in.GlobalValue("left")
					if err != nil {
						t.Fatal(err)
					}
				}
				before := len(rt.refStore.byToken)
				for _, off := range []int{0, 8, 16, 24, 32} {
					binary.LittleEndian.PutUint64(row[off:], binary.LittleEndian.Uint64(original[off:])^1)
					_, err := in.GlobalValue("left")
					matches := cached && in.FuncRefMatchesFunction(token.FuncRef(), 0)
					var aliasErr error
					if cached {
						_, aliasErr = alias.GlobalValue("right")
					}
					copy(row, original)
					if err == nil || matches || cached && aliasErr == nil || len(rt.refStore.byToken) != before {
						t.Fatal("unauthenticated host proxy/canonical slot", cached, off, err, aliasErr)
					}
				}
			}
		})
	}
}

func TestRV64PublicHostReferenceTokenBindingGate(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt, foreign := NewRuntime(), NewRuntime()
			defer rt.Close()
			defer foreign.Close()
			c := rv64HostTokenCompile(t, 0, artifact)
			fn := slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p) })
			host := rv64HostTokenOwner(t, foreign, ValI32, fn)
			closed := rv64HostTokenOwner(t, rt, ValI32, fn)
			closed.Close()
			for _, h := range []*HostFuncRef{host, closed} {
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				c.codeCache.mu.Lock()
				base, mem, refs, sealed := c.codeCache.base, offHeapSlicePtr(c.codeCache.mem), c.codeCache.refs, c.codeCache.sealed
				c.codeCache.mu.Unlock()
				in, err := rt.Instantiate(context.Background(), mod, WithImports(rv64HostTokenImports(h)))
				c.codeCache.mu.Lock()
				unchanged := base == c.codeCache.base && mem == offHeapSlicePtr(c.codeCache.mem) && refs == c.codeCache.refs && sealed == c.codeCache.sealed
				c.codeCache.mu.Unlock()
				mod.Close()
				if in != nil {
					in.Close()
				}
				if err == nil || in != nil || !unchanged || h.importers != 0 || len(rt.refStore.byToken) != 0 {
					t.Fatal("invalid owned-host binding published", err)
				}
			}
		})
	}
}
