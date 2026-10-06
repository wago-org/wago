//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"slices"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

type swarHostSignature struct{ params, results []wasm.ValType }

func swarImportSignature(sig *wasm.CompType) error {
	return swarCallSignature(sig)
}

// Generate bounded numeric/vector transport templates, with an image budget already
// reduced by every admitted defined function. Empty inputs retain nil images.
func swarHostThunks(signatures []swarHostSignature, limit int) ([]byte, []int, error) {
	if len(signatures) > MaxHostFunctionImports || limit < 0 || limit > MaxCodeBytes {
		return nil, nil, unsupported("SWAR host thunk resource bound")
	}
	for _, sig := range signatures {
		if err := swarImportSignature(&wasm.CompType{Kind: wasm.CompFunc, Params: sig.params, Results: sig.results}); err != nil {
			return nil, nil, err
		}
	}
	var code []byte
	var entries []int
	for i, sig := range signatures {
		body := HostIndirectSyncThunk(uint32(i), swarSignatureSlots(sig.params), swarSignatureSlots(sig.results))
		padding := (-len(code)) & 15
		if len(body) == 0 || len(body)&3 != 0 || len(body) > limit-len(code)-padding {
			return nil, nil, unsupported("SWAR host thunk image bound")
		}
		for ; padding > 0; padding -= 4 {
			code = append(code, 0x13, 0, 0, 0)
		}
		entries = append(entries, len(code))
		code = append(code, body...)
	}
	return code, entries, nil
}

// This private owner requires exclusive idle live JobMemory, no saved/queued
// contexts, and every invocation joined before Close. Caller-owned guest/thunk
// CodeBuffers stay unchanged/live through all sharing owners' Close. Their real
// W^X/process-wide cache synchronization completes before either binding. The
// opaque context is an identity token, not a public cross-instance dispatch API.
type swarHostOwner struct {
	arena               *coreruntime.Arena
	job                 *coreruntime.JobMemory
	guest, thunks       *coreruntime.CodeBuffer
	dispatch, directory uintptr
}

func (image *swarModuleImage) instantiateHosts(job *coreruntime.JobMemory, guest, thunks *coreruntime.CodeBuffer) (*swarHostOwner, error) {
	return image.instantiateHostsWithArena(job, guest, thunks, coreruntime.NewArena)
}

func (image *swarModuleImage) instantiateHostsWithArena(job *coreruntime.JobMemory, guest, thunks *coreruntime.CodeBuffer, allocate func(int) (*coreruntime.Arena, error)) (*swarHostOwner, error) {
	if image == nil || job == nil || len(image.hosts) > MaxHostFunctionImports || image.table != nil || len(image.elements) != 0 || len(image.functionKeys) != 0 {
		return nil, unsupported("SWAR host owner image/job/table shape")
	}
	ctx := job.CaptureInstanceContext()
	if ctx.ImportDispatch != 0 || ctx.FuncRefDescPtr != 0 || ctx.TablePtr != 0 || ctx.PassiveElemPtr != 0 {
		return nil, unsupported("SWAR host owner requires unbound host/reference job")
	}
	if len(image.hosts) == 0 {
		if len(image.hostCode) != 0 || len(image.hostEntries) != 0 {
			return nil, unsupported("SWAR host owner missing signatures")
		}
		return &swarHostOwner{}, nil
	}
	if allocate == nil || guest == nil || thunks == nil || len(image.code) == 0 || len(image.code) > MaxCodeBytes || len(image.hostCode) > MaxCodeBytes-len(image.code) || len(image.functions) == 0 || len(image.functions) > MaxFunctions-len(image.hosts) || !bytes.Equal(guest.Bytes(), image.code) || !bytes.Equal(thunks.Bytes(), image.hostCode) {
		return nil, unsupported("SWAR host owner code/directory bound")
	}
	previousEnd := 0
	for i, info := range image.functions {
		if info.Entry < previousEnd || info.Entry&15 != 0 || info.CodeBytes <= 0 || info.CodeBytes&3 != 0 || info.Entry > len(image.code) || info.CodeBytes > len(image.code)-info.Entry || info.FrameHeaderBytes != callFrameHeader || info.FrameBytes < callFrameHeader || info.FrameBytes > MaxFrameBytes || info.FrameBytes&15 != 0 || !info.mode.production || !info.mode.interruptible || info.mode.functionIndex != uint32(len(image.hosts)+i) || info.Params < 0 || info.Params > 256 || info.Results < 0 || info.Results > 256 {
			return nil, unsupported("SWAR host owner framed defined entries")
		}
		previousEnd = info.Entry + info.CodeBytes
	}
	// Reproduce the bounded thunk layout from all declared carrier signatures,
	// including unused imports, before sealing or allocating any owner arena.
	expected, entries, err := swarHostThunks(image.hosts, MaxCodeBytes-len(image.code))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(expected, image.hostCode) || !slices.Equal(entries, image.hostEntries) {
		return nil, unsupported("SWAR host owner signature/thunk layout")
	}
	if err := guest.Seal(); err != nil {
		return nil, err
	}
	if err := thunks.Seal(); err != nil {
		return nil, err
	}
	if guest.Base() == 0 || guest.Base()&15 != 0 || thunks.Base() == 0 || thunks.Base()&15 != 0 {
		return nil, unsupported("SWAR host owner executable alignment")
	}
	arena, err := allocate(len(image.hosts)*coreruntime.ImportDispatchEntryBytes + coreruntime.FuncRefDescBytes + 8)
	if err != nil {
		return nil, err
	}
	dispatch := arena.Alloc(len(image.hosts) * coreruntime.ImportDispatchEntryBytes)
	directory, context := arena.Alloc(coreruntime.FuncRefDescBytes), arena.Alloc(8)
	clear(dispatch)
	clear(directory)
	clear(context)
	identity := uint64(uintptr(unsafe.Pointer(&context[0])))
	binary.LittleEndian.PutUint64(directory[coreruntime.FuncRefContextOffset:], identity)
	for i, entry := range image.hostEntries {
		row := dispatch[i*coreruntime.ImportDispatchEntryBytes:]
		binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchCodePtrOffset:], uint64(thunks.Base())+uint64(entry))
		binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchHomeLinMemOffset:], uint64(job.LinMemBase()))
		binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchTargetContextOffset:], identity)
		binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchCallerContextOffset:], identity)
	}
	owner := &swarHostOwner{arena: arena, job: job, guest: guest, thunks: thunks, dispatch: uintptr(unsafe.Pointer(&dispatch[0])), directory: uintptr(unsafe.Pointer(&directory[0]))}
	job.SetFuncRefDesc(owner.directory)
	job.SetImportDispatchPtr(owner.dispatch)
	return owner, nil
}

func (owner *swarHostOwner) Close() error { return owner.closeWith((*coreruntime.Arena).Close) }
func (owner *swarHostOwner) closeWith(release func(*coreruntime.Arena) error) error {
	if owner == nil || owner.arena == nil {
		return nil
	}
	ctx := owner.job.CaptureInstanceContext()
	if ctx.ImportDispatch != 0 && ctx.ImportDispatch != owner.dispatch || ctx.FuncRefDescPtr != 0 && ctx.FuncRefDescPtr != owner.directory {
		return unsupported("SWAR host owner binding replaced")
	}
	owner.job.SetImportDispatchPtr(0)
	owner.job.SetFuncRefDesc(0)
	if err := release(owner.arena); err != nil {
		return err
	}
	owner.arena, owner.job, owner.guest, owner.thunks = nil, nil, nil, nil
	owner.dispatch, owner.directory = 0, 0
	return nil
}
