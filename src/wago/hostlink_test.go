//go:build linux && amd64 && !tinygo

package wago

import (
	"context"
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type exactHostCallbackSlotsExtension struct {
	params  int
	results int
}

func (*exactHostCallbackSlotsExtension) Info() ExtensionInfo {
	return ExtensionInfo{ID: "test.exact-host-callback-slots", RequiresCapabilities: []PluginCapability{PluginHostImports}}
}

func (e *exactHostCallbackSlotsExtension) Register(reg *Registry) error {
	host, err := reg.HostImports()
	if err != nil {
		return err
	}
	host.CallerResolver() // force synchronous host linking for exact identity
	host.Module("env").Func("step", func(_ HostModule, params, results []uint64) {
		e.params, e.results = len(params), len(results)
		results[0] = params[0] + 1
	}).Params(ValI32).Results(ValI32)
	return nil
}

func TestSynchronousHostCallbackUsesDeclaredSlotWidths(t *testing.T) {
	ext := &exactHostCallbackSlotsExtension{}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.Use(ext, WithPluginGrants(PluginHostImports)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("step")...), 0x00, 0x00)
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}))),
	))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer in.Close()
	results, err := in.Call(context.Background(), "run", ValueI32(41))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(results) != 1 || results[0].I32() != 42 {
		t.Fatalf("results = %v, want [i32(42)]", results)
	}
	if ext.params != 1 || ext.results != 1 {
		t.Fatalf("host callback slots = params %d, results %d; want 1, 1", ext.params, ext.results)
	}
}

func TestCallerResolverSyncLinkCacheClosesWithCompiled(t *testing.T) {
	c := MustCompile(voidImportCallModule())
	imports := Imports{"env.f": HostFunc(func(HostModule, []uint64, []uint64) {})}
	linked, err := c.linkModuleMode(imports, nil, true)
	if err != nil {
		t.Fatalf("forced synchronous link: %v", err)
	}
	if linked == c || !linked.syncHostImports {
		t.Fatalf("forced link = %p sync=%v, want distinct synchronous module", linked, linked.syncHostImports)
	}
	if c.hostLink == nil || c.hostLink.syncC != linked {
		t.Fatal("forced synchronous link was not memoized")
	}
	linked.ensureCodeCache()
	if err := c.Close(); err != nil {
		t.Fatalf("Compiled.Close: %v", err)
	}
	linked.codeCache.mu.Lock()
	closed := linked.codeCache.closed
	linked.codeCache.mu.Unlock()
	if !closed {
		t.Fatal("Compiled.Close left forced synchronous linked code open")
	}
}

// TestHostLinkCached verifies the host-only link recompile is memoized: a
// needsLink module (returning imports) links once and every later host
// Instantiate reuses that linked module + its code mapping instead of re-running
// the backend. Guards the large-module instantiate optimization.
func TestHostLinkCached(t *testing.T) {
	src, err := os.ReadFile("../../bench/corpus/jsonproc.wasm")
	if err != nil {
		t.Skip("jsonproc.wasm not present")
	}
	c, err := Compile(nil, src)
	if err != nil {
		t.Fatal(err)
	}
	if !c.needsLink || c.hostLink == nil {
		t.Fatalf("expected a deferred-codegen (needsLink) module with a host-link cache")
	}
	// Satisfy the module's imports with bare stubs; this test exercises link
	// caching, not host-import behavior, so the imports need only bind.
	stubs := Imports{}
	for _, name := range c.Imports {
		stubs[name] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}
	l1, err := c.linkModule(stubs, nil)
	if err != nil {
		t.Fatalf("link 1: %v", err)
	}
	l2, err := c.linkModule(stubs, nil)
	if err != nil {
		t.Fatalf("link 2: %v", err)
	}
	if l1 == c {
		t.Fatal("host link should recompile to a fresh linked module")
	}
	if l1 != l2 {
		t.Fatal("host link not cached: repeated linkModule produced different linked modules")
	}
}
