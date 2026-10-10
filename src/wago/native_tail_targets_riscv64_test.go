//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestRV64PrivateTailTargetDirectory(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, kind := range []string{"go", "owned", "native"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, kind), func(t *testing.T) {
				native := kind == "native"
				types := []wasm.ValType{wasm.I32}
				c := rv64NativeTailCompile(t, "c2k-directory-"+kind+"-consumer", rv64TableFunctionImportModule(types, false, false, nil, false, true), artifact)
				imports := NewImports()
				var producer *Instance
				var ownedRuntime *Runtime
				if native {
					p := rv64NativeTailCompile(t, "c2k-directory-native-producer", rv64PublicNativeIdentityModule(types), artifact)
					var err error
					producer, err = Instantiate(p, NewImports())
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					export, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					imports.Function("env", "host", export)
				} else if kind == "owned" {
					rt := NewRuntime()
					ownedRuntime = rt
					defer rt.Close()
					owner, err := rt.NewHostFuncRef(HostCallFunc(func(call HostCall) { call.ResultSlots()[0] = call.ParamSlots()[0] }), FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
					if err != nil {
						t.Fatal(err)
					}
					defer owner.Close()
					imports.Function("env", "host", owner)
				} else {
					imports.Function("env", "host", HostCallFunc(func(call HostCall) { call.ResultSlots()[0] = call.ParamSlots()[0] }))
				}
				var in *Instance
				var err error
				if kind == "owned" {
					in = rv64HostTailInstantiate(t, ownedRuntime, c, imports)
				} else {
					in, err = Instantiate(c, imports)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if err = rv64ValidateTailTargets(in); err != nil {
					t.Fatal(err)
				}
				if native {
					for _, region := range [][]byte{producer.funcRefDescs[16:24], rv64NativeInvocationContext(producer)[16:24]} {
						before := binary.LittleEndian.Uint64(region)
						binary.LittleEndian.PutUint64(region, before^0x8000000000000001)
						if err = rv64ValidateTailTargets(in); err == nil {
							t.Fatal("malformed native target anchor accepted")
						}
						binary.LittleEndian.PutUint64(region, before)
					}
				}
				d := in.pluginState.Load().railshotInvocation.tailTargets
				row := d[abi.RV64TailTargetsHeaderBytes:]
				ordinary := binary.LittleEndian.Uint64(in.funcRefDescs[coreruntime.FuncRefDescBytes:])
				if native {
					if binary.LittleEndian.Uint64(row) != uint64(producer.base+uintptr(producer.c.Entry[0])) || binary.LittleEndian.Uint64(row[8:]) != uint64(producer.jm.LinMemBase()) || binary.LittleEndian.Uint64(row[16:]) != uint64(producer.nativeContext) || binary.LittleEndian.Uint64(row[24:]) != 0 || binary.LittleEndian.Uint64(row) == ordinary {
						t.Fatal("native tail row did not bypass the linked importer bridge")
					}
					if err = producer.Close(); err != nil {
						t.Fatal(err)
					}
				} else if binary.LittleEndian.Uint64(row) != ordinary || binary.LittleEndian.Uint64(row[8:]) != uint64(in.jm.LinMemBase()) || binary.LittleEndian.Uint64(row[16:]) != uint64(in.nativeContext) || binary.LittleEndian.Uint64(row[24:]) != abi.RV64TailTargetsHost {
					t.Fatal("host tail row lost its retained importer wrapper")
				}
				if got, err := in.Invoke("call", 0xfeedface00000007, 0); err != nil || len(got) != 2 || got[0] != 7 || got[1] != 9 {
					t.Fatal("ordinary proxy semantics", got, err)
				}
				// Authentication uses retained slice extents and attachments. No bad
				// pointer below is dereferenced, and restoring bytes permits cold reuse.
				for _, region := range [][]byte{d[:32], row[:32], in.funcRefDescs[8:16]} {
					for offset := 0; offset < len(region); offset += 8 {
						before := binary.LittleEndian.Uint64(region[offset:])
						binary.LittleEndian.PutUint64(region[offset:], before^0x8000000000000001)
						if err = rv64ValidateTailTargets(in); err == nil {
							t.Fatal("changed retained directory accepted", offset)
						}
						binary.LittleEndian.PutUint64(region[offset:], before)
					}
				}
				if err = rv64ValidateTailTargets(in); err != nil {
					t.Fatal(err)
				}
				if got, err := in.Invoke("call", 8, 0); err != nil || got[0] != 8 {
					t.Fatal("cold restored directory", got, err)
				}
				if err = in.Close(); err != nil {
					t.Fatal(err)
				}
				if in.pluginState.Load().railshotInvocation.tailTargets != nil || in.funcRefDescs != nil {
					t.Fatal("borrowed directory retained after physical retirement")
				}
			})
		}
	}
}

func TestRV64PrivateTailTargetDirectoryQuota(t *testing.T) {
	c := rv64PublicStartCompile(t, rv64TableFunctionImportModule([]wasm.ValType{wasm.I32}, false, false, nil, false, true), false)
	imports := NewImports().Function("env", "host", HostCallFunc(func(call HostCall) { call.ResultSlots()[0] = call.ParamSlots()[0] }))
	view := c.executionView()
	if err := view.validateCached(); err != nil {
		t.Fatal(err)
	}
	resolved, err := imports.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	need := view.arenaNeedForImports(resolved, true) + railshotNativeImportMetadataBytes(view, resolved)
	if railshotNativeTailArenaBytes(view) != abi.RV64TailTargetsHeaderBytes+abi.RV64TailTargetsRowBytes*len(view.FuncTypeID) {
		t.Fatal("directory footprint not charged")
	}
	for _, limit := range []int{need - 1, need} {
		rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithMaxInstanceMetadataBytes(uint64(limit))))
		mod, err := rt.Module(c)
		if err != nil {
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
		if limit < need {
			var quota *coreruntime.ResourceLimitError
			if in != nil || !errors.As(err, &quota) || quota.Requested != uint64(need) {
				if in != nil {
					in.Close()
				}
				t.Fatal("directory quota denial", in != nil, need, err)
			}
		} else {
			if err != nil || in == nil {
				t.Fatal("exact directory quota", err)
			}
			if err = rv64ValidateTailTargets(in); err != nil {
				t.Fatal(err)
			}
			in.Close()
		}
		mod.Close()
		rt.Close()
	}
}

// This is a directory eligibility probe, not reference-signature execution.
// Source/public admission continues to reject reference-valued functions.
func TestRV64PrivateTailTargetDirectoryUnsupportedRows(t *testing.T) {
	base := rv64PublicStartCompile(t, rv64TableFunctionImportModule([]wasm.ValType{wasm.I32}, false, false, nil, false, true), false)
	for _, typ := range []ValType{ValFuncRef, ValExternRef, ValV128} {
		view := *base.executionView()
		view.FuncTypeID = view.FuncTypeID[:1]
		view.Funcs, view.Entry = nil, nil
		view.importFuncSigs = []FuncSig{{Params: []ValType{typ}, Results: []ValType{typ}}}
		ar, err := coreruntime.NewArena(80 + 64)
		if err != nil {
			t.Fatal(err)
		}
		defer ar.Close()
		j, err := coreruntime.NewJobMemory(0)
		if err != nil {
			t.Fatal(err)
		}
		defer j.Close()
		in := &Instance{c: &view, ar: ar, jm: j, nativeContext: 0x1000, funcRefDescs: ar.Alloc(80), syncMode: true}
		if err = rv64PrepareTailTargets(in, nil, 0, nil, nil); err != nil {
			t.Fatal("unsupported eligibility must stay empty", typ, err)
		}
		d := in.pluginState.Load().railshotInvocation.tailTargets
		if !rv64EmptyTailTargetRow(d[32:]) || rv64ValidateTailTargets(in) != nil {
			t.Fatal("unsupported tail row publication", typ)
		}
		binary.LittleEndian.PutUint64(d[32:], 0x2000)
		if rv64ValidateTailTargets(in) == nil {
			t.Fatal("unsupported tail row acquired an entry", typ)
		}
		railshotReleaseNativeTail(in)
		if in.pluginState.Load().railshotInvocation.tailTargets != nil {
			t.Fatal("unsupported row cleanup retained arena")
		}
	}
	view := *base.executionView()
	view.FuncTypeID = nil
	if rv64TailTargetsArenaBytes(&view) != 0 {
		t.Fatal("empty indexed arena allocated a directory")
	}
}
