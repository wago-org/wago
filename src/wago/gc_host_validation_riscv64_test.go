//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRV64ForeignImportDescriptorRequiresFrozenWrapper(t *testing.T) {
	owners, rt := NewRuntime(), NewRuntime()
	defer owners.Close()
	defer rt.Close()
	pc := rv64TableStageCompile(t, "foreign-wrapper-validation-producer", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	defer pc.Close()
	producer := rv64NativeTokenInstantiate(t, owners, pc, NewImports())
	export, err := producer.ExportedFunc("identity")
	if err != nil {
		t.Fatal(err)
	}
	cc := rv64TableStageCompile(t, "foreign-wrapper-validation-consumer", rv64TableFunctionImportModule([]wasm.ValType{wasm.I32}, false, false, nil, false, true), false)
	defer cc.Close()
	in := rv64NativeTokenInstantiate(t, rt, cc, NewImports().Function("env", "host", export))
	producer.Close()
	descriptor := uint64(offHeapSlicePtr(in.funcRefDescs)) + coreruntime.FuncRefDescBytes
	owner := in.refStore.retainDescriptorOwnerForFinalization(descriptor)
	if owner != in {
		t.Fatalf("foreign wrapper physical owner=%p want importer %p", owner, in)
	}
	owner.releaseResourceRoot()
	entry := in.funcRefDescs[coreruntime.FuncRefDescBytes : 2*coreruntime.FuncRefDescBytes]
	for name, offset := range map[string]int{"code": coreruntime.TableEntryCodePtrOffset, "home": coreruntime.TableEntryHomeLinMemOffset, "signature": coreruntime.TableEntrySigKeyOffset, "identity": coreruntime.TableEntryRefSlotOffset, "context": coreruntime.FuncRefContextOffset} {
		t.Run(name, func(t *testing.T) {
			before := in.referenceLifetime().snapshot()
			original := binary.LittleEndian.Uint64(entry[offset:])
			binary.LittleEndian.PutUint64(entry[offset:], original^16)
			retained := in.refStore.retainDescriptorOwnerForFinalization(descriptor)
			binary.LittleEndian.PutUint64(entry[offset:], original)
			if retained != nil {
				retained.releaseResourceRoot()
				t.Fatal("changed foreign wrapper descriptor was retained")
			}
			if after := in.referenceLifetime().snapshot(); after != before {
				t.Fatal("rejected descriptor changed lifetime roots")
			}
		})
	}
}

func TestRV64PlainHostDescriptorValidationPreservesGCLease(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	compiled := rv64TableStageCompile(t, "plain-host-descriptor-validation", rv64LocalMutableReferenceImportModule(false), false)
	defer compiled.Close()
	imports := NewImports()
	imports.Function("env", "target", slotHostFunc(func(HostModule, []uint64, []uint64) {}))
	in := rv64NativeTokenInstantiate(t, rt, compiled, imports)
	defer in.Close()
	descriptor := uint64(offHeapSlicePtr(in.funcRefDescs)) + coreruntime.FuncRefDescBytes
	entry := in.funcRefDescs[coreruntime.FuncRefDescBytes : 2*coreruntime.FuncRefDescBytes]
	for name, offset := range map[string]int{
		"code":      coreruntime.TableEntryCodePtrOffset,
		"home":      coreruntime.TableEntryHomeLinMemOffset,
		"signature": coreruntime.TableEntrySigKeyOffset,
		"identity":  coreruntime.TableEntryRefSlotOffset,
		"context":   coreruntime.FuncRefContextOffset,
	} {
		t.Run(name, func(t *testing.T) {
			original := binary.LittleEndian.Uint64(entry[offset:])
			binary.LittleEndian.PutUint64(entry[offset:], original^16)
			defer binary.LittleEndian.PutUint64(entry[offset:], original)
			in.refStore.mu.Lock()
			_, _, valid := in.refStore.canonicalFuncrefOwnerLocked(in, descriptor)
			in.refStore.mu.Unlock()
			if valid {
				t.Fatal("changed descriptor admitted")
			}
		})
	}
	topology := in.refStore.gcDomains
	var validationPanic any
	func() {
		id := newInvocationID()
		lease := in.lockGCInvocation(id)
		defer lease.unlock()
		activation := hostLoopActivation{root: in, ctrl: offHeapSlicePtr(in.ctrl), invocation: hostInvocationContext{id: id}}
		func() {
			defer func() { validationPanic = recover() }()
			activation.dispatch(activation.ctrl, hostFuncRefDispatchBit, []uint64{0, 0, 0}, nil)
		}()
		topology.mu.Lock()
		readers := topology.readers
		topology.mu.Unlock()
		if readers != 1 {
			t.Errorf("validation consumed acquired reader: readers=%d", readers)
		}
	}()
	validation, ok := validationPanic.(invalidHostReference)
	if !ok || !strings.Contains(validation.err.Error(), "invalid RV64 guest global store bridge") {
		t.Fatalf("validation panic=%v", validationPanic)
	}
	topology.mu.Lock()
	readers, writer := topology.readers, topology.writer
	topology.mu.Unlock()
	if readers != 0 || writer {
		t.Fatalf("validation left topology readers=%d writer=%t", readers, writer)
	}
	importedGlobalInvoke(t, in, "set", []uint64{0})
	importedGlobalInvoke(t, in, "saved", []uint64{0})
}
