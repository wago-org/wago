//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"slices"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

// Built once alongside immutable wrapper bytes. Published only after the owning
// mapping is RX and process-wide instruction-cache synchronization has succeeded.
// The shared form lives in the compiled cache; private owned wrappers live in
// the importing instance's construction record. Neither carries a callback root.
type railshotHostThunkConstruction struct {
	records   []rv64HostThunkRecord
	codeBytes int
}

type rv64HostThunkRecord struct {
	certificate   *railshot.NativeTailHostCertificate
	offset        int
	dispatchIndex uint32
	owned         bool
}

func railshotInitHostThunkConstruction(s *railshotHostThunkConstruction, c *Compiled) {
	if c.needsFuncRefDescs() {
		s.records = make([]rv64HostThunkRecord, c.NumImports)
	}
}

func railshotCertifiedHostThunk(s *railshotHostThunkConstruction, index int, dispatch uint32, params, results int, owned bool) ([]byte, error) {
	if len(s.records) == 0 || params > 64 || results > 64 {
		if owned {
			return railshotHostIndirectOwnedSyncThunk(dispatch, params, results), nil
		}
		return railshotHostIndirectSyncThunk(dispatch, params, results), nil
	}
	code, cert, err := railshot.NativeTailHostThunkWithCertificate(dispatch, params, results, owned)
	if err == nil {
		s.records[index] = rv64HostThunkRecord{certificate: cert, dispatchIndex: dispatch, owned: owned}
	}
	return code, err
}

func railshotOwnedHostThunk(s *railshotNativeImportConstruction, index int, dispatch uint32, params, results int) ([]byte, error) {
	return railshotCertifiedHostThunk(&s.hosts, index, dispatch, params, results, true)
}

func railshotRecordHostThunkSpan(s *railshotHostThunkConstruction, index, offset int) {
	if len(s.records) != 0 && s.records[index].certificate != nil {
		s.records[index].offset = offset
	}
}

func railshotFinishHostThunkConstruction(s *railshotHostThunkConstruction, size int) {
	if len(s.records) != 0 {
		s.codeBytes = size
	}
}

// A wrapper image alone cannot identify a callback: many instances share its
// bytes. This adapter freezes the particular instance's immutable binding and
// ordered signature. It borrows the invocation graph's physical lease, and must
// be validated under nativeExecutionMu on entry and after every host park.
type rv64HostTailCodeOwner struct {
	instance        *Instance
	index           int
	record          rv64HostThunkRecord
	binding         *syncHostBinding
	host            *HostFuncRef
	job             *coreruntime.JobMemory
	context, anchor uintptr
	signature       FuncSig
}

type rv64HostTailAttachment struct {
	record  rv64HostThunkRecord
	view    []byte
	binding *syncHostBinding
	host    *HostFuncRef
}

// Count-only admission and warm refresh use the same nonallocating ownership
// checks. Authenticate a particular physical instance before touching any of its
// off-heap state; never substitute a sibling instance's shared cache reference.
func rv64HostTailAttached(in *Instance, index int) (a rv64HostTailAttachment, err error) {
	if _, err = (rv64TailCodeOwner{in}).nativeOwnerCodeView(true); err != nil {
		return a, err
	}
	if !in.syncMode || in.hostEvents != nil || index < 0 || index >= in.c.NumImports || index >= len(in.c.importFuncSigs) || index >= len(in.syncHosts) || index >= len(in.importReferenceEntries()) {
		return a, rv64TailGenerationError("host attachment extent/transport")
	}
	sig := in.c.importFuncSigs[index]
	if err := rv64ScalarHostSignature(sig); err != nil && !rv64PublicHostReferenceBoundary(in.c) {
		return a, err
	}
	entry := in.importReferenceEntries()[index]
	a.binding, a.host = &in.syncHosts[index], entry.host
	if entry.native != nil || !a.binding.callable() || a.binding.importIdx != uint32(index) || a.binding.sig == nil || !slices.Equal(sig.Params, a.binding.sig.Params) || !slices.Equal(sig.Results, a.binding.sig.Results) {
		return a, rv64TailGenerationError("host immutable import binding")
	}
	if a.host != nil {
		a.view, err = (rv64TailThunkCodeOwner{in}).NativeTailCodeView()
		if err != nil {
			return a, err
		}
		s := in.pluginState.Load().railshotInvocation.nativeImports
		if len(s.hosts.records) != in.c.NumImports || s.hosts.codeBytes != len(a.view) {
			return a, rv64TailGenerationError("owned host construction extent")
		}
		a.record = s.hosts.records[index]
		if !a.record.owned || a.record.dispatchIndex&hostFuncRefDispatchBit == 0 {
			return a, rv64TailGenerationError("owned host dispatch class")
		}
		bound, ok := in.boundHostFuncRef(a.record.dispatchIndex)
		if !ok || bound.owner != a.host || !slices.Equal(sig.Params, bound.sig.Params) || !slices.Equal(sig.Results, bound.sig.Results) {
			return a, rv64TailGenerationError("owned host resolver identity/signature")
		}
	} else {
		cc := in.c.loadCodeCache()
		cc.mu.Lock()
		defer cc.mu.Unlock()
		memo := in.c.loadValidateMemo()
		if memo == nil {
			return a, rv64TailGenerationError("shared host construction missing")
		}
		cache := &memo.hostThunks[1]
		if cache.base == 0 || cache.base&15 != 0 || offHeapSlicePtr(cache.mem) != cache.base || cache.codeBytes <= 0 || cache.codeBytes > len(cache.mem) || len(cache.records) != in.c.NumImports || len(cache.offsets) != in.c.NumImports {
			return a, rv64TailGenerationError("shared host published image extent")
		}
		a.record = cache.records[index]
		if a.record.owned || a.record.dispatchIndex != uint32(index) || a.record.offset != cache.offsets[index] {
			return a, rv64TailGenerationError("shared host dispatch identity")
		}
		a.view = cache.mem[:cache.codeBytes:cache.codeBytes]
	}
	r := a.record
	if r.certificate == nil || r.offset < 0 || r.offset&15 != 0 || r.offset > len(a.view) || r.certificate.CodeBytes() > len(a.view)-r.offset || entry.address != uint64(offHeapSlicePtr(a.view)+uintptr(r.offset)) {
		return a, rv64TailGenerationError("host canonical wrapper extent/address")
	}
	return a, nil
}

func rv64HostTailCodeOwnerBytes(values int) (int, error) {
	if values < 0 || values > 128 {
		return 0, rv64TailGenerationError("host owner signature count")
	}
	return int(unsafe.Sizeof(rv64HostTailCodeOwner{})) + values*int(unsafe.Sizeof(ValType(0))), nil
}

// The generation reserves these bytes before any signature conversion/copy.
func rv64NewHostTailCodeOwner(in *Instance, index int) (*rv64HostTailCodeOwner, error) {
	a, err := rv64HostTailAttached(in, index)
	if err != nil {
		return nil, err
	}
	sig := in.c.importFuncSigs[index]
	sig.Params, sig.Results = slices.Clone(sig.Params), slices.Clone(sig.Results)
	return &rv64HostTailCodeOwner{instance: in, index: index, record: a.record, binding: a.binding, host: a.host, job: in.jm, context: in.nativeContext, anchor: offHeapSlicePtr(in.funcRefDescs), signature: sig}, nil
}

func (o *rv64HostTailCodeOwner) NativeTailCodeView() ([]byte, error) {
	if o == nil {
		return nil, rv64TailGenerationError("missing host code owner")
	}
	a, err := rv64HostTailAttached(o.instance, o.index)
	if err != nil {
		return nil, err
	}
	in := o.instance
	sig := in.c.importFuncSigs[o.index]
	if a.record != o.record || a.binding != o.binding || a.host != o.host || in.jm != o.job || in.nativeContext != o.context || offHeapSlicePtr(in.funcRefDescs) != o.anchor || !slices.Equal(sig.Params, o.signature.Params) || !slices.Equal(sig.Results, o.signature.Results) {
		return nil, rv64TailGenerationError("host owner binding changed")
	}
	return a.view, nil
}
