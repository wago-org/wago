//go:build ((linux && (amd64 || riscv64)) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcHostReentryFrameRootModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	hostType := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	outerType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	outerLocals := []byte{0x01, 0x01, 0x63, 0x00}
	outer := append([]byte(nil), outerLocals...)
	outer = append(outer,
		0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x10, 0x00, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	)
	innerLocals := []byte{0x01, 0x01, 0x7f}
	inner := append([]byte(nil), innerLocals...)
	inner = append(inner,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0x00, 0x41, 0xe8, 0x07, 0x4f, 0x0d, 0x01,
		0xfb, 0x01, 0x00, 0xd1, 0x1a,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x21, 0x00, 0x0c, 0x00,
		0x0b, 0x0b, 0x41, 0x00, 0x0b,
	)
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("reenter")...), 0x00)
	imp = append(imp, wasmtest.ULEB(1)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, hostType, outerType)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("outer", 0, 1), wasmtest.ExportEntry("inner", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(outer))), outer...),
			append(wasmtest.ULEB(uint32(len(inner))), inner...),
		)),
	)
}

func TestGCHostReentryFullDepth(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcHostReentryFrameRootModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	plan := compiled.genericGCFrameRoots()
	if plan == nil || len(plan.callsites) != 1 || len(plan.callsites[0].offsets) != 1 {
		t.Fatalf("host reentry native root map = %+v", plan)
	}
	profiles := []struct {
		name   string
		config GCConfig
	}{
		{"throughput", GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, profile := range profiles {
		for _, callerAware := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/caller=%t", profile.name, callerAware), func(t *testing.T) {
				var in *Instance
				active, calls := 0, 0
				overflow := false
				callback := func(caller HostModule) {
					active++
					calls++
					defer func() { active-- }()
					// Each nested outer allocates under collection pressure while
					// its callers keep their struct roots live across callbacks.
					// The outer callback plus all permitted reentries must fit.
					if active == maxHostReentryDepth+1 && !overflow {
						return
					}
					want := uint64(active)
					got, err := in.InvokeFromHost(context.Background(), caller, "outer", want)
					if err != nil {
						panic(HostTrap{Err: err})
					}
					if len(got) != 1 || got[0] != want {
						panic(fmt.Sprintf("outer = %v, want [%d]", got, want))
					}
				}
				var host any
				if callerAware {
					host = CallerHostCallFunc(func(caller Caller, call HostCall) {
						callback(caller)
						call.ResultSlots()[0] = 0
					})
				} else {
					host = slotHostFunc(func(caller HostModule, _, results []uint64) {
						callback(caller)
						results[0] = 0
					})
				}
				in, err = Instantiate(compiled, InstantiateOptions{GC: profile.config, Imports: testImports("env.reenter", host)})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				// Recover after the rejected call on the same instance as well.
				for _, reject := range []bool{false, true, false} {
					overflow, calls = reject, 0
					got, err := in.Invoke("outer", 91)
					if reject {
						if !errors.Is(err, ErrPermissionDenied) {
							t.Fatalf("overflow = %v, want ErrPermissionDenied", err)
						}
					} else if err != nil || len(got) != 1 || got[0] != 91 {
						t.Fatalf("full-depth reentry = %v, %v; want [91], nil", got, err)
					}
					if calls != maxHostReentryDepth+1 || active != 0 {
						t.Fatalf("callbacks = %d, active = %d; want %d, 0", calls, active, maxHostReentryDepth+1)
					}
					state := in.publicGCState()
					state.mu.Lock()
					pending := state.hostActivationCount
					state.mu.Unlock()
					if pending != 0 {
						t.Fatalf("retained %d host activations", pending)
					}
					assertHostReentryDepthsEmpty(t)
				}
			})
		}
	}
}
