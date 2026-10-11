//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Only indexed canonical arenas need a target directory. Descriptor-zero-only
// producers still have their context anchor, without an extra allocation.
func rv64TailTargetsArenaBytes(c *Compiled) int {
	if !c.needsFuncRefDescs() || len(c.FuncTypeID) == 0 {
		return 0
	}
	return abi.RV64TailTargetsHeaderBytes + abi.RV64TailTargetsRowBytes*len(c.FuncTypeID)
}

func rv64QualifiedNativeTailTarget(target *InstanceExport) (entry, home, context uint64, valid bool) {
	if target == nil || target.inst == nil || target.inst.jm == nil || target.inst.c == nil || target.localIdx < 0 || target.localIdx >= len(target.inst.c.Entry) || target.localIdx >= len(target.inst.c.Funcs) {
		return
	}
	producer := target.inst
	if railshotNonCallableFunction(producer.c, producer.c.NumImports+target.localIdx) {
		return
	}
	code, err := (rv64TailCodeOwner{producer}).NativeTailCodeView()
	if err != nil {
		return
	}
	if !producer.c.nativeContextTrapABI || !producer.c.registerABIDisabled || len(producer.funcRefDescs) < coreruntime.FuncRefDescBytes || producer.nativeContext == 0 || producer.nativeContext&7 != 0 || binary.LittleEndian.Uint64(producer.funcRefDescs[coreruntime.FuncRefContextOffset:]) != uint64(producer.nativeContext) || binary.LittleEndian.Uint64(producer.funcRefDescs[coreruntime.TableEntryHomeLinMemOffset:]) != uint64(producer.jm.LinMemBase()) || binary.LittleEndian.Uint64(rv64NativeInvocationContext(producer)[16:]) != uint64(offHeapSlicePtr(producer.funcRefDescs)) {
		return
	}
	offset := producer.c.Entry[target.localIdx]
	if producer.base == 0 || offset < 0 || offset&15 != 0 || offset >= len(code) {
		return
	}
	if _, err := railshot.NativeFrameBytes(code, offset); err != nil {
		return
	}
	return uint64(producer.base) + uint64(offset), uint64(producer.jm.LinMemBase()), uint64(producer.nativeContext), true
}

// Ordinary imported descriptors continue to identify importer-owned wrappers.
// Their separate tail rows resolve frozen attachments to genuine defined native
// entries or retained synchronous host wrappers. No linked import bridge, raw Go
// address, or process pointer is encoded into a compiled artifact.
func rv64PrepareTailTargets(in *Instance, imports resolvedImports, sharedBase uintptr, sharedOffsets []int, ownedAddr map[uint32]uint64) error {
	size := rv64TailTargetsArenaBytes(in.c)
	if size == 0 {
		return nil
	}
	if len(in.c.FuncTypeID) > railshot.MaxFunctions+railshot.MaxHostFunctionImports || len(in.funcRefDescs) != coreruntime.FuncRefDescBytes*(len(in.c.FuncTypeID)+1) {
		return fmt.Errorf("RV64 tail target directory count/extent")
	}
	directory := in.ar.Alloc(size)
	clear(directory)
	binary.LittleEndian.PutUint64(directory, abi.RV64TailTargetsMagic)
	binary.LittleEndian.PutUint32(directory[8:], abi.RV64TailTargetsVersion)
	binary.LittleEndian.PutUint32(directory[12:], uint32(len(in.c.FuncTypeID)))
	binary.LittleEndian.PutUint64(directory[16:], uint64(offHeapSlicePtr(in.funcRefDescs)))
	binary.LittleEndian.PutUint64(directory[24:], uint64(in.nativeContext))
	for function := range in.c.FuncTypeID {
		if railshotNonCallableFunction(in.c, function) {
			continue
		}
		var entry, home, context, flags uint64
		if local := function - in.c.NumImports; local >= 0 {
			if local >= len(in.c.Entry) || local >= len(in.c.Funcs) {
				return fmt.Errorf("RV64 tail target function directory")
			}
			entry = uint64(in.base) + uint64(in.c.Entry[local])
			home, context = uint64(in.jm.LinMemBase()), uint64(in.nativeContext)
		} else if target, native := imports[in.c.functionImportBindingKey(function)].(*InstanceExport); native {
			var valid bool
			entry, home, context, valid = rv64QualifiedNativeTailTarget(target)
			if !valid {
				continue // Keep the ordinary proxy; do not publish a tail entry.
			}
		} else {
			if err := rv64ScalarHostSignature(in.c.importFuncSigs[function]); err != nil && !rv64PublicHostReferenceBoundary(in.c) && !rv64PublicDynamicHostReferenceBoundary(in.c) {
				// Preserve the ordinary descriptor; this signature has no
				// qualified tail transport yet.
				continue
			}
			if !in.syncMode {
				return fmt.Errorf("RV64 tail target requires synchronous host transport")
			}
			entry = ownedAddr[uint32(function)]
			if entry == 0 && function < len(sharedOffsets) && sharedOffsets[function] != noHostThunkOffset {
				entry = uint64(sharedBase + uintptr(sharedOffsets[function]))
			}
			home, context, flags = uint64(in.jm.LinMemBase()), uint64(in.nativeContext), abi.RV64TailTargetsHost
		}
		if entry == 0 || entry&15 != 0 || home == 0 || home&7 != 0 || context == 0 || context&7 != 0 {
			return fmt.Errorf("RV64 tail target entry/identity alignment")
		}
		row := directory[abi.RV64TailTargetsHeaderBytes+function*abi.RV64TailTargetsRowBytes:]
		binary.LittleEndian.PutUint64(row, entry)
		binary.LittleEndian.PutUint64(row[8:], home)
		binary.LittleEndian.PutUint64(row[16:], context)
		binary.LittleEndian.PutUint64(row[24:], flags)
	}
	// Install after the complete directory is validated, before any initializer
	// or public instance entry. Arena lifetime and quota cover every byte.
	binary.LittleEndian.PutUint64(in.funcRefDescs[abi.RV64TailTargetsAnchorOffset:], uint64(offHeapSlicePtr(directory)))
	in.ensurePluginState().railshotInvocation.tailTargets = directory
	return nil
}

// Check retained slices and frozen attachments, never a pointer supplied by a
// table row. Native code may read a row only after this owning graph has been
// acquired and authenticated at entry/resume under nativeExecutionMu.
func rv64ValidateTailTargets(in *Instance) error {
	size := rv64TailTargetsArenaBytes(in.c)
	if size == 0 {
		return nil
	}
	state := in.pluginState.Load()
	if state == nil {
		return fmt.Errorf("RV64 tail target directory has no owner")
	}
	d := state.railshotInvocation.tailTargets
	if len(d) != size || len(in.funcRefDescs) != coreruntime.FuncRefDescBytes*(len(in.c.FuncTypeID)+1) || binary.LittleEndian.Uint64(in.funcRefDescs[abi.RV64TailTargetsAnchorOffset:]) != uint64(offHeapSlicePtr(d)) || binary.LittleEndian.Uint64(d) != abi.RV64TailTargetsMagic || binary.LittleEndian.Uint32(d[8:]) != abi.RV64TailTargetsVersion || binary.LittleEndian.Uint32(d[12:]) != uint32(len(in.c.FuncTypeID)) || binary.LittleEndian.Uint64(d[16:]) != uint64(offHeapSlicePtr(in.funcRefDescs)) || binary.LittleEndian.Uint64(d[24:]) != uint64(in.nativeContext) {
		return fmt.Errorf("RV64 tail target directory identity/extent")
	}
	for function := range in.c.FuncTypeID {
		row := d[abi.RV64TailTargetsHeaderBytes+function*abi.RV64TailTargetsRowBytes:]
		if railshotNonCallableFunction(in.c, function) {
			descriptor := in.funcRefDescs[(function+1)*coreruntime.FuncRefDescBytes:]
			if !rv64EmptyTailTargetRow(row) || !rv64EmptyTailTargetRow(descriptor) || binary.LittleEndian.Uint64(descriptor[coreruntime.FuncRefContextOffset:]) != 0 {
				return fmt.Errorf("RV64 internal reference descriptor and tail rows must be empty")
			}
			continue
		}
		entry, home, context, flags := binary.LittleEndian.Uint64(row), uint64(in.jm.LinMemBase()), uint64(in.nativeContext), uint64(0)
		var expected uint64
		if local := function - in.c.NumImports; local >= 0 {
			if local >= len(in.c.Entry) {
				return fmt.Errorf("RV64 tail target local function index")
			}
			expected = uint64(in.base) + uint64(in.c.Entry[local])
		} else if target, native := rv64NativeFunctionAttachment(in, function); native {
			var valid bool
			expected, home, context, valid = rv64QualifiedNativeTailTarget(target)
			if !valid {
				if !rv64EmptyTailTargetRow(row) {
					return fmt.Errorf("RV64 unsupported native tail row must be empty")
				}
				continue
			}
		} else {
			if rv64ScalarHostSignature(in.c.importFuncSigs[function]) != nil && !rv64PublicHostReferenceBoundary(in.c) && !rv64PublicDynamicHostReferenceBoundary(in.c) {
				if !rv64EmptyTailTargetRow(row) {
					return fmt.Errorf("RV64 unsupported host tail row must be empty")
				}
				continue
			}
			if function >= len(in.importReferenceEntries()) {
				return fmt.Errorf("RV64 tail target host attachment")
			}
			expected, flags = in.importReferenceEntries()[function].address, abi.RV64TailTargetsHost
		}
		if entry != expected || expected == 0 || expected&15 != 0 || binary.LittleEndian.Uint64(row[8:]) != home || binary.LittleEndian.Uint64(row[16:]) != context || binary.LittleEndian.Uint64(row[24:]) != flags {
			return fmt.Errorf("RV64 tail target differs from retained attachment")
		}
	}
	return nil
}

func rv64EmptyTailTargetRow(row []byte) bool {
	return binary.LittleEndian.Uint64(row) == 0 && binary.LittleEndian.Uint64(row[8:]) == 0 && binary.LittleEndian.Uint64(row[16:]) == 0 && binary.LittleEndian.Uint64(row[24:]) == 0
}
