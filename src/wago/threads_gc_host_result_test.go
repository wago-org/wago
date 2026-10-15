//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestThreadsSharedForeignGCResultsUseOwnActivation(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureGC)
	requireCompleteCore3Backend(t)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3 | CoreFeatureThreads).WithBoundsChecks(BoundsChecksExplicit)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	entered := make(chan uint64, 2)
	release := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	defer func() {
		for _, ch := range release {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
	}()
	def := testDefinition("example.com/threads-gc-result")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "test concurrent GC host results", Scope: AuthorityScope{Modules: []string{"plugin_gc"}}}}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			imports, err := reg.HostImports()
			if err != nil {
				return err
			}
			callerTestDeclare(imports, "plugin_gc", false)("create", func(h HostModule, args, results []uint64) {
				index := args[0]
				entered <- index
				select {
				case <-release[index]:
				case <-time.After(3 * time.Second):
					panic(HostTrap{Err: fmt.Errorf("GC callback release timed out")})
				}
				allocator := h.(GuestGCArrayAllocatorHostModule)
				token, err := allocator.NewGCArrayResult(0, 1, func(payload []byte, _ GuestGCArrayInfo) error {
					payload[0] = byte(42 + index)
					return nil
				})
				pluginGCHostTrap(err)
				results[0] = token
			}).Params(ValI32).Results(ValAnyRef)
			return nil
		})
	}}
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x78, 0}, []byte{0x60, 1, 0x7f, 1, 0x63, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(pluginGCImport("plugin_gc", "create", 1))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2})),
		wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x41, 0, 0xfb, 0x0d, 0, 0x0b}))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	target, err := in.ExportedFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	relayMod, err := rt.Compile(threadsImportedHostMinimalModule())
	if err != nil {
		t.Fatal(err)
	}
	defer relayMod.Close()
	done := [2]chan error{make(chan error, 1), make(chan error, 1)}
	for index := range done {
		relay, err := rt.Instantiate(context.Background(), relayMod, WithImports(testImports("env.host", target)))
		if err != nil {
			t.Fatal(err)
		}
		defer relay.Close()
		go func(index int, relay *Instance) {
			got, err := relay.Invoke("run", uint64(index))
			if err == nil && (len(got) != 1 || got[0] != uint64(42+index)) {
				err = fmt.Errorf("GC result %d: %v", index, got)
			}
			done[index] <- err
		}(index, relay)
		select {
		case got := <-entered:
			if got != uint64(index) {
				t.Fatalf("callback = %d", got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("callback did not enter")
		}
	}
	close(release[0])
	if err := <-done[0]; err != nil {
		t.Fatal(err)
	}
	state := in.existingPublicGCState()
	state.mu.Lock()
	remainingRoots := uint8(0)
	for _, count := range state.hostResultRootCount {
		remainingRoots += count
	}
	state.mu.Unlock()
	if remainingRoots != 0 {
		t.Errorf("completed callback left %d result roots on another parked activation", remainingRoots)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	close(release[1])
	if err := <-done[1]; err != nil {
		t.Fatal(err)
	}
}
