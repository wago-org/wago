//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// These are trusted, exclusively owned private proof inputs. Code/context/job
// mappings remain live and unchanged through owner.Close; their external owners
// may logically retire while a lease is held but must not physically unmap them.
// Nothing here is a public attachment API or a serialized address contract.
type tailRAFunction struct {
	code               *coreruntime.CodeBuffer
	snapshot           []byte
	info               FunctionInfo
	signature          *wasm.CompType
	job                *coreruntime.JobMemory
	context, directory []byte
}

func tailRAPointer(b []byte) uintptr { return uintptr(unsafe.Pointer(unsafe.SliceData(b))) }

func (f tailRAFunction) validate(caller bool) error {
	minimum := coreruntime.InstanceContextBytes
	if caller {
		minimum = tailRAContextBytes
	}
	if f.code == nil || f.code.Base() == 0 || f.code.Base()&15 != 0 || f.job == nil || f.job.LinMemBase() == 0 || len(f.snapshot) == 0 || len(f.snapshot) > MaxCodeBytes || !bytes.Equal(f.code.Bytes(), f.snapshot) || len(f.context) < minimum || tailRAPointer(f.context)&7 != 0 || len(f.directory) < 40 || tailRAPointer(f.directory)&7 != 0 {
		return unsupported("private tail frozen code/context/owner")
	}
	i := f.info
	if !i.mode.production || !i.mode.interruptible || i.Entry < 0 || i.Entry&15 != 0 || i.CodeBytes <= 0 || i.CodeBytes&3 != 0 || i.Entry > len(f.snapshot) || i.CodeBytes > len(f.snapshot)-i.Entry || i.FrameBytes < 16 || i.FrameBytes > MaxFrameBytes || i.FrameBytes&15 != 0 || i.FrameHeaderBytes != 16 && i.FrameHeaderBytes != callFrameHeader || caller && i.FrameHeaderBytes != callFrameHeader {
		return unsupported("private tail framed entry")
	}
	if err := tailRASignature(f.signature); err != nil {
		return err
	}
	if i.Params != swarSignatureSlots(f.signature.Params) || i.Results != swarSignatureSlots(f.signature.Results) {
		return unsupported("private tail physical signature metadata")
	}
	if binary.LittleEndian.Uint64(f.context[16:]) != uint64(tailRAPointer(f.directory)) || binary.LittleEndian.Uint64(f.directory[32:]) != uint64(tailRAPointer(f.context)) {
		return unsupported("private tail canonical context anchor")
	}
	// Existing production framing begins with the authoritative fence load.
	// The trusted compiler's immutable FunctionInfo/source snapshot is retained;
	// a claimed arbitrary instruction address is not accepted as a framed entry.
	var expected enc.Asm
	expected.Ld(enc.X29, enc.X25, -72)
	if i.CodeBytes < 4 || !bytes.Equal(f.snapshot[i.Entry:i.Entry+4], expected.B) {
		return unsupported("private tail production entry fingerprint")
	}
	return nil
}

type tailRAOwnerSite struct {
	caller, target           tailRAFunction
	sourcePC                 uint32
	sourceBase, sourceStride int
}

type tailRAOwnerSpec struct {
	sites              []tailRAOwnerSite
	exit               *coreruntime.CodeBuffer
	arguments, results []tailRARange
	continuations      []tailRARange
	// All original continuation and buffer ranges have explicit physical owners
	// retained by this invocation lease. They may include process root assembly,
	// a caller image or an importing bridge. No target-image inference is used.
	retained                             []any
	stackLimit, stackTop, hostTrampoline uintptr
}

// Resource roots live here, never in a native-stack record. One exclusive native
// stack/execution owner must hold the single lease through native/park/resume.
// acquire/release must
// surround complete native/park/resume/cold decoration; Close refuses an active
// lease. Externally owned input maps are not freed by this private owner.
type tailRAOwner struct {
	mu         sync.Mutex
	active     int
	ready      bool
	closed     bool
	code       *coreruntime.CodeBuffer
	arena      *coreruntime.Arena
	capability []byte
	contexts   [][]byte
	spec       tailRAOwnerSpec
}

func tailRAOwnedBound(s tailRAOwnerSpec) (int, int, error) {
	if len(s.sites) == 0 || len(s.sites) > tailRAMaxSites || len(s.arguments) > tailRAMaxRanges || len(s.results) > tailRAMaxRanges || len(s.continuations) == 0 || len(s.continuations) > tailRAMaxRanges || len(s.retained) == 0 || len(s.retained) > tailRAMaxRanges || s.exit == nil || s.stackLimit == 0 || s.stackLimit&15 != 0 || s.stackTop&15 != 0 || s.stackTop <= s.stackLimit || s.stackTop-s.stackLimit < tailRARecordBytes+16 || s.hostTrampoline == 0 || s.hostTrampoline&3 != 0 {
		return 0, 0, unsupported("private tail owner counts/lease/stack")
	}
	exitCode, err := tailRAExitCode()
	if err != nil {
		return 0, 0, err
	}
	if !bytes.Equal(s.exit.Bytes(), exitCode) || s.exit.Base() == 0 || s.exit.Base()&15 != 0 {
		return 0, 0, unsupported("private tail exact exit image")
	}
	metadata := tailRACapHeader + len(s.sites)*(tailRASiteRow+int(unsafe.Sizeof(tailRAOwnerSite{}))) + len(s.retained)*16
	if metadata > MaxModuleBytes {
		return 0, 0, unsupported("private tail metadata bound")
	}
	rangeCount := len(s.arguments) + len(s.results) + len(s.continuations)
	if rangeCount > (MaxModuleBytes-metadata)/16 {
		return 0, 0, unsupported("private tail metadata bound")
	}
	metadata += 16 * rangeCount
	for _, list := range [][]tailRARange{s.arguments, s.results} {
		for _, r := range list {
			if !r.valid(8) {
				return 0, 0, unsupported("private tail arena range")
			}
		}
	}
	for _, r := range s.continuations {
		if !r.valid(4) {
			return 0, 0, unsupported("private tail continuation range")
		}
	}
	codeBytes := len(exitCode)
	seenCode := make(map[*coreruntime.CodeBuffer]bool)
	perFunction := make(map[struct {
		code  *coreruntime.CodeBuffer
		entry int
	}]int)
	for _, site := range s.sites {
		if err := site.caller.validate(true); err != nil {
			return 0, 0, err
		}
		if err := site.target.validate(false); err != nil {
			return 0, 0, err
		}
		if len(site.caller.context) != tailRAContextBytes || binary.LittleEndian.Uint64(site.caller.context[tailRACapOffset:]) != 0 {
			return 0, 0, unsupported("private tail caller context extension already bound")
		}
		if len(site.caller.signature.Results) != len(site.target.signature.Results) {
			return 0, 0, unsupported("private tail exact ordered results")
		}
		for i, typ := range site.caller.signature.Results {
			if !wasm.EqualValType(typ, site.target.signature.Results[i]) {
				return 0, 0, unsupported("private tail exact ordered results")
			}
		}
		key := struct {
			code  *coreruntime.CodeBuffer
			entry int
		}{site.caller.code, site.caller.info.Entry}
		perFunction[key]++
		if perFunction[key] > maxDirectCalls || len(perFunction) > MaxFunctions {
			return 0, 0, unsupported("private tail per-function site bound")
		}
		for _, f := range []tailRAFunction{site.caller, site.target} {
			if !seenCode[f.code] {
				if len(f.snapshot) > MaxCodeBytes-codeBytes {
					return 0, 0, unsupported("private tail combined code bound")
				}
				codeBytes += len(f.snapshot)
				seenCode[f.code] = true
			}
		}
		values := len(site.target.signature.Params) + len(site.target.signature.Results)
		if values > (MaxModuleBytes-metadata)/16 {
			return 0, 0, unsupported("private tail metadata bound")
		}
		metadata += values*16 + 128
		if metadata > MaxModuleBytes {
			return 0, 0, unsupported("private tail metadata bound")
		}
		bound := tailRADispatchBound + 128*rangeCount
		if bound > MaxCodeBytes-codeBytes {
			return 0, 0, unsupported("private tail combined dispatcher bound")
		}
		codeBytes += bound
		p := tailRAProofSite{frameBytes: site.caller.info.FrameBytes, functionIndex: site.caller.info.mode.functionIndex, sourcePC: site.sourcePC, callerHome: site.caller.job.LinMemBase(), callerContext: tailRAPointer(site.caller.context), target: nativeImportTarget{entry: site.target.code.Base() + uintptr(site.target.info.Entry), home: site.target.job.LinMemBase(), context: tailRAPointer(site.target.context), hostTrampoline: s.hostTrampoline}, params: site.target.signature.Params, results: site.target.signature.Results, sourceBase: site.sourceBase, sourceStride: site.sourceStride, argumentRanges: s.arguments, resultRanges: s.results, continuationRanges: s.continuations, stackLimit: s.stackLimit, stackTop: s.stackTop}
		if err := p.validate(); err != nil {
			return 0, 0, err
		}
	}
	return metadata, codeBytes, nil
}

func newTailRAOwner(s tailRAOwnerSpec) (*tailRAOwner, error) {
	return newTailRAOwnerWith(s, coreruntime.NewArena, coreruntime.NewCodeBuffer)
}

func newTailRAOwnerWith(s tailRAOwnerSpec, allocate func(int) (*coreruntime.Arena, error), mapCode func(int) (*coreruntime.CodeBuffer, error)) (*tailRAOwner, error) {
	return newTailRAOwnerPublishWith(s, allocate, mapCode, (*coreruntime.CodeBuffer).Seal)
}

func newTailRAOwnerPublishWith(s tailRAOwnerSpec, allocate func(int) (*coreruntime.Arena, error), mapCode func(int) (*coreruntime.CodeBuffer, error), publish func(*coreruntime.CodeBuffer) error) (*tailRAOwner, error) {
	_, bound, err := tailRAOwnedBound(s)
	if err != nil {
		return nil, err
	}
	if allocate == nil || mapCode == nil || publish == nil {
		return nil, unsupported("private tail owner allocators")
	}
	// Every untrusted count/shape/budget check precedes per-site generation, Seal or
	// an executable/data allocation. External code owners remain responsible for
	// their own failed Seal mappings; nothing has been bound to a context yet.
	if err := s.exit.Seal(); err != nil {
		return nil, err
	}
	seen := make(map[*coreruntime.CodeBuffer]bool)
	for _, site := range s.sites {
		for _, f := range []tailRAFunction{site.caller, site.target} {
			if !seen[f.code] {
				if err := f.code.Seal(); err != nil {
					return nil, err
				}
				seen[f.code] = true
			}
		}
	}
	o := &tailRAOwner{spec: s}
	// Freeze bounded mutable caller slices before publishing any capability.
	o.spec.arguments = append([]tailRARange(nil), s.arguments...)
	o.spec.results = append([]tailRARange(nil), s.results...)
	o.spec.continuations = append([]tailRARange(nil), s.continuations...)
	o.spec.retained = append([]any(nil), s.retained...)
	o.spec.sites = append([]tailRAOwnerSite(nil), s.sites...)
	helperCapacity := bound - len(s.exit.Bytes())
	seen = make(map[*coreruntime.CodeBuffer]bool)
	for _, site := range s.sites {
		for _, f := range []tailRAFunction{site.caller, site.target} {
			if !seen[f.code] {
				helperCapacity -= len(f.snapshot)
				seen[f.code] = true
			}
		}
	}
	o.code, err = mapCode(helperCapacity)
	if err != nil {
		if o.code != nil {
			return o, err
		}
		return nil, err
	}
	if o.code == nil || o.code.Base() == 0 || len(o.code.Bytes()) != 0 || len(o.code.Mapping()) < helperCapacity {
		return o, unsupported("private tail stable empty dispatcher mapping")
	}
	base := o.code.Base()
	o.arena, err = allocate(tailRACapHeader + len(s.sites)*tailRASiteRow)
	if err != nil {
		return o, err
	}
	if o.arena == nil {
		return o, unsupported("private tail missing capability arena")
	}
	o.capability = o.arena.Alloc(tailRACapHeader + len(s.sites)*tailRASiteRow)
	clear(o.capability)
	binary.LittleEndian.PutUint64(o.capability, tailRACapMagic)
	binary.LittleEndian.PutUint32(o.capability[8:], tailRAVersion)
	binary.LittleEndian.PutUint32(o.capability[12:], uint32(len(s.sites)))
	exit := s.exit.Base()
	for i, site := range s.sites {
		p := tailRAProofSite{frameBytes: site.caller.info.FrameBytes, functionIndex: site.caller.info.mode.functionIndex, sourcePC: site.sourcePC, callerHome: site.caller.job.LinMemBase(), callerContext: tailRAPointer(site.caller.context), target: nativeImportTarget{entry: site.target.code.Base() + uintptr(site.target.info.Entry), home: site.target.job.LinMemBase(), context: tailRAPointer(site.target.context), hostTrampoline: s.hostTrampoline}, params: append([]wasm.ValType(nil), site.target.signature.Params...), results: append([]wasm.ValType(nil), site.target.signature.Results...), sourceBase: site.sourceBase, sourceStride: site.sourceStride, argumentRanges: o.spec.arguments, resultRanges: o.spec.results, continuationRanges: o.spec.continuations, stackLimit: s.stackLimit, stackTop: s.stackTop}
		finalize, err := tailRAFinalize(p, exit)
		if err != nil {
			return o, err
		}
		for len(o.code.Bytes())&15 != 0 {
			if err := o.code.Append([]byte{0x13, 0, 0, 0}); err != nil {
				return o, err
			}
		}
		finalAddress := o.code.Base() + uintptr(len(o.code.Bytes()))
		if err := o.code.Append(finalize); err != nil {
			return o, err
		}
		preflight, err := tailRAPreflight(p, exit, finalAddress)
		if err != nil {
			return o, err
		}
		if len(preflight)+len(finalize)+32 > tailRADispatchBound+128*(len(s.arguments)+len(s.results)+len(s.continuations)) {
			return o, unsupported("private tail dispatcher conservative bound exceeded")
		}
		for len(o.code.Bytes())&15 != 0 {
			if err := o.code.Append([]byte{0x13, 0, 0, 0}); err != nil {
				return o, err
			}
		}
		preAddress := o.code.Base() + uintptr(len(o.code.Bytes()))
		if err := o.code.Append(preflight); err != nil {
			return o, err
		}
		row := o.capability[tailRACapHeader+i*tailRASiteRow:]
		binary.LittleEndian.PutUint64(row, uint64(preAddress))
		binary.LittleEndian.PutUint64(row[8:], uint64(finalAddress))
		found := false
		for _, ctx := range o.contexts {
			if tailRAPointer(ctx) == tailRAPointer(site.caller.context) {
				found = true
				break
			}
		}
		if !found {
			o.contexts = append(o.contexts, site.caller.context)
		}
	}
	if len(o.code.Bytes()) > helperCapacity || o.code.Base() != base {
		return o, unsupported("private tail owned code actual bound")
	}
	if err := publish(o.code); err != nil {
		return o, err
	}
	for _, ctx := range o.contexts {
		binary.LittleEndian.PutUint64(ctx[tailRACapOffset:], uint64(tailRAPointer(o.capability)))
	}
	o.ready = true
	return o, nil
}

func (o *tailRAOwner) acquire() error {
	if o == nil {
		return unsupported("private tail missing lease")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.ready || o.closed || o.active != 0 || o.code == nil || o.code.Base() == 0 || o.arena == nil {
		return unsupported("private tail retired lease")
	}
	for _, ctx := range o.contexts {
		if binary.LittleEndian.Uint64(ctx[tailRACapOffset:]) != uint64(tailRAPointer(o.capability)) {
			return unsupported("private tail capability binding replaced")
		}
	}
	o.active++
	return nil
}

func (o *tailRAOwner) release() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active == 0 {
		panic("private tail unbalanced lease")
	}
	o.active--
}

func (o *tailRAOwner) Close() error {
	return o.closeWith((*coreruntime.CodeBuffer).Close, (*coreruntime.Arena).Close)
}
func (o *tailRAOwner) closeWith(closeCode func(*coreruntime.CodeBuffer) error, closeArena func(*coreruntime.Arena) error) error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.active != 0 {
		return unsupported("private tail active invocation")
	}
	for _, ctx := range o.contexts {
		v := binary.LittleEndian.Uint64(ctx[tailRACapOffset:])
		if v != 0 && v != uint64(tailRAPointer(o.capability)) {
			return unsupported("private tail binding replaced")
		}
	}
	// Detach before freeing. Failed cleanup retains the lease's complete resource
	// references and mappings for a later retry; cold bypass requires no exit.
	for _, ctx := range o.contexts {
		binary.LittleEndian.PutUint64(ctx[tailRACapOffset:], 0)
	}
	o.closed = true
	o.ready = false
	var errs []error
	if o.code != nil {
		if err := closeCode(o.code); err != nil {
			errs = append(errs, err)
		} else {
			o.code = nil
		}
	}
	if o.arena != nil {
		if err := closeArena(o.arena); err != nil {
			errs = append(errs, err)
		} else {
			o.arena = nil
			o.capability = nil
		}
	}
	if len(errs) == 0 {
		o.contexts = nil
		o.spec = tailRAOwnerSpec{}
	}
	return errors.Join(errs...)
}
