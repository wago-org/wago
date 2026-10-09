//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceResourceGrower(wide bool) []byte {
	typ, flag := wasm.I32, byte(1)
	if wide {
		typ = wasm.I64
		flag = 5
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))), wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("memory")...), 2, flag, 1, 3))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("grow", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x40, 0, 0x0b}))))
}

func rv64ReferenceResourceImportBindings(t *testing.T, resource *Instance) (*Imports, []*Memory) {
	t.Helper()
	im := NewImports()
	var memories []*Memory
	for i := 0; i < 2; i++ {
		m, err := resource.ExportedMemory(fmt.Sprintf("memory.%d", i))
		if err != nil {
			t.Fatal(err)
		}
		memories = append(memories, m)
		im.Memory("env.resources", fmt.Sprintf("memory.%d", i), m)
	}
	im.Memory("env.resources", "alias.1", memories[1])
	for _, name := range []string{"seed", "counter", "payload", "offset"} {
		g, err := resource.ExportedGlobalObject(name)
		if err != nil {
			t.Fatal(err)
		}
		im.Global("env.resources", name, g)
	}
	return im, memories
}

func TestRV64PublicReferenceFunctionResourceImportsSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, wide := range []bool{false, true} {
					for _, route := range []string{"direct", "foreign-distinct", "foreign-same"} {
						foreign := route != "direct"
						sharedHome := route == "foreign-same"
						t.Run(fmt.Sprintf("artifact%t-vector%t-tails%t-wide%t-%s", artifact, vector, tails, wide, route), func(t *testing.T) {
							rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
							defer rv64HostCertificateCloseRuntime(t, rt)
							rc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-provider-v%t-w%t", vector, wide), wasmtest.ClosedNumericImportResources(vector, wide), artifact)
							resource := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
							if len(resource.c.Funcs) != 0 || resource.c.NumImports != 0 || resource.base != 0 {
								t.Fatal("resource-only provider acquired function code")
							}
							im, memories := rv64ReferenceResourceImportBindings(t, resource)

							gc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-grower-w%t", !wide), rv64ReferenceResourceGrower(!wide), artifact)
							grower := rv64NativeTokenInstantiate(t, rt, gc, NewImports().Memory("env", "memory", memories[1]))
							nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
							native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
							nf, err := native.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							entered, release := make(chan struct{}), make(chan struct{})
							var parkOnce, releaseOnce sync.Once
							unblock := func() { releaseOnce.Do(func() { close(release) }) }
							defer unblock()
							shared := HostCallFunc(func(call HostCall) {
								if call.ParamSlots()[0] != 0xfffffffd {
									panic("resource shared input")
								}
								parkOnce.Do(func() { close(entered); <-release })
								runtime.GC()
								call.ResultSlots()[0] = 0xdeadbeeffffffffe
							})
							pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-imports-v%t-t%t-w%t", vector, tails, wide), wasmtest.ClosedReferenceResourceImports(vector, tails, wide), artifact)
							owned := rv64DynamicHostBinding(t, rt, true, HostCallFunc(func(call HostCall) {
								if call.ParamSlots()[0] != 0xfffffffe {
									panic("resource owned input")
								}
								call.ResultSlots()[0] = 0xaabbccdd00000000
							}), pc.importFuncSigs[2])
							im.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", shared).Function("env.calls", "owned.fn", owned)
							selected := rv64NativeTokenInstantiate(t, rt, pc, im)
							if selected.jm != resource.jm || selected.memoryDir.memories[1].jobMemory() != selected.memoryDir.memories[2].jobMemory() || selected.c.NumImports != 3 || len(selected.c.GlobalImports) != 4 || selected.c.memoryImportCount() != 3 {
								t.Fatal("resource alias/import namespace")
							}
							if readGlobalObject(selected.globalCells[5], ValI32) != 11 {
								t.Fatal("owned imported-global initializer")
							}
							caller := selected
							name := "run"
							if foreign {
								target, err := selected.ExportedFunc("run")
								if err != nil {
									t.Fatal(err)
								}
								cc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-caller-v%t-s%t-w%t", vector, sharedHome, wide), rv64ReferenceImportsConsumerWidth(vector, sharedHome, wide, "reference-tail"), artifact)
								callerImports := NewImports().Function("env", "target", target)
								if sharedHome {
									callerImports.Memory("env", "memory", memories[0])
								}
								caller = rv64NativeTokenInstantiate(t, rt, cc, callerImports)
								if (caller.jm == selected.jm) != sharedHome {
									t.Fatal("foreign memory home")
								}
								name = "call"
								func() {
									gate := caller.lockInvocation(0)
									defer gate.unlockInvocation()
									entry, err := caller.beginNativeEntry()
									if err != nil {
										t.Fatal(err)
									}
									defer caller.unlockNativeEntry(entry)
									found := false
									for _, g := range entry.tables.targets.tailActive {
										for i, owner := range g.owners {
											if owner == resource {
												t.Fatal("resource-only provider became callable snapshot owner")
											}
											if owner == selected {
												found = true
												if g.snapshots[i] == nil || g.snapshots[i].Validate() != nil {
													t.Fatal("resource sparse snapshot")
												}
											}
										}
									}
									if !found {
										t.Fatal("selected sparse imported owner missing")
									}
								}()
							}
							args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
							payload := []uint64{args[1]}
							if vector {
								args = append(args, 0x80000000deadbeef)
								payload = append(payload, args[2])
							}
							want := append(slices.Clone(payload), 9, 0)
							args = append(args, 2)
							type result struct {
								values []uint64
								err    error
							}
							done := make(chan result, 1)
							finished := make(chan struct{})
							go func() { defer close(finished); v, err := caller.Invoke(name, args...); done <- result{v, err} }()
							defer func() {
								unblock()
								select {
								case <-finished:
								case <-time.After(5 * time.Second):
									t.Error("resource import invocation did not join")
								}
							}()
							select {
							case <-entered:
							case <-time.After(5 * time.Second):
								t.Fatal("resource import did not park")
							}
							if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
								t.Fatal("non-primary alias growth while parked", got, err)
							}
							// Seed distinct bytes in the newly grown page while the native
							// continuation is parked, then observe them before any guest growth.
							resumed := []uint64{0x7ff00000feedbeef}
							if vector {
								resumed = append(resumed, 0x80000001cafebabe)
							}
							var resumedBytes []byte
							for _, v := range resumed {
								resumedBytes = binary.LittleEndian.AppendUint64(resumedBytes, v)
							}
							copy(memories[1].UnsafeBytes()[65536+64:], resumedBytes)
							if err := resource.Close(); err != nil {
								t.Fatal(err)
							}
							if err := rc.Close(); err != nil {
								t.Fatal(err)
							}
							if err := native.Close(); err != nil {
								t.Fatal(err)
							}
							if err := nc.Close(); err != nil {
								t.Fatal(err)
							}
							if resource.resourcesClosed || native.resourcesClosed {
								t.Fatal("parked resource graph lost storage")
							}
							unblock()
							select {
							case got := <-done:
								if got.err != nil || !slices.Equal(got.values, want) {
									t.Fatal("resumed resource reference call", got.values, got.err)
								}
							case <-time.After(5 * time.Second):
								t.Fatal("resumed resource import timeout")
							}
							if caller == selected {
								rv64ReferenceResourceImportCarriers(t, selected, 2)
							}
							if readGlobalObject(selected.globalCells[1], ValI32) != 9 || readGlobalObject(selected.globalCells[4], ValI32) != 0 || readGlobalObject(selected.globalCells[6], ValI64) != 2 {
								t.Fatal("resumed aliased state/size")
							}
							if vector {
								if got := readGlobalObjectV128(selected.globalCells[7]); !bytes.Equal(got[:], resumedBytes) {
									t.Fatal("resumed alias vector", got)
								}
							} else if got := readGlobalObject(selected.globalCells[7], ValF64); got != resumed[0] {
								t.Fatal("resumed alias scalar", got)
							}
							var encoded []byte
							for _, v := range payload {
								encoded = binary.LittleEndian.AppendUint64(encoded, v)
							}
							for _, i := range []int{0, 1, 2} {
								// The selected attachment still owns physical storage after provider Close.
								b := selected.memoryDir.memories[i].jobMemory().HostBytes()
								expected := 2
								if i != 0 {
									expected = 3
								}
								if len(b) != expected*65536 || !bytes.Equal(b[64:64+len(encoded)], encoded) {
									t.Fatal("resumed alias reads and payload", i, len(b))
								}
							}
							for _, choice := range []uint64{1, 3, 4, 0} {
								args[len(args)-1] = choice
								null := uint64(0)
								if choice == 1 {
									null = 1
								}
								want[len(want)-1] = null
								if got, err := caller.Invoke(name, args...); err != nil || !slices.Equal(got, want) || readGlobalObject(selected.globalCells[4], ValI32) != null {
									t.Fatal("retired resource/descriptor reuse", choice, got, err)
								}
								if caller == selected {
									rv64ReferenceResourceImportCarriers(t, selected, choice)
								}
							}
							if foreign {
								if err := selected.Close(); err != nil {
									t.Fatal(err)
								}
								if selected.resourcesClosed {
									t.Fatal("foreign resource owner released")
								}
								if err := caller.Close(); err != nil {
									t.Fatal(err)
								}
							} else {
								if err := selected.Close(); err != nil {
									t.Fatal(err)
								}
							}
							if !selected.resourcesClosed || resource.resourcesClosed {
								t.Fatal("independent resource borrower lifetime")
							}
							if err := grower.Close(); err != nil {
								t.Fatal(err)
							}
							if !resource.resourcesClosed || !native.resourcesClosed {
								t.Fatal("final resource roots retained")
							}
						})
					}
				}
			}
		}
	}
}

// The final private two-reference call writes its exact arguments into the
// direct invocation arena; this is an internal oracle, never a public reference
// signature. Imports and local descriptors must retain nonzero upper bits.
func rv64ReferenceResourceImportCarriers(t *testing.T, selected *Instance, choice uint64) {
	t.Helper()
	want := uint64(0)
	if choice != 1 {
		index := 4
		if choice >= 2 {
			index = int(choice) - 2
		}
		want = uint64(offHeapSlicePtr(selected.funcRefDescs[40*(index+1) : 40*(index+2)]))
		if want>>32 == 0 {
			t.Fatal("resource descriptor lacks upper bits")
		}
	}
	// Foreign tail transport belongs to its temporary activation instead; the
	// foreign cases check saved-local nullness and exact numeric results.
	if len(selected.serArgs) < 16 {
		t.Fatal("resource private argument arena")
	}
	if binary.LittleEndian.Uint64(selected.serArgs) != want || binary.LittleEndian.Uint64(selected.serArgs[8:]) != want {
		t.Fatal("resource import reference bits", choice, want, selected.serArgs[:16])
	}
}
