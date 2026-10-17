//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/codeimage"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeTailHostCertificate identifies bytes issued by the synchronous host
// wrapper builder. It owns no mapping or callback. Runtime users must retain
// the particular instance, host binding and wrapper image through every park.
// dispatchIndex is the host resolver's index, not a Wasm function index.
// No field or process address is serialized into a compiled artifact.
type NativeTailHostCertificate struct {
	digest                     codeimage.IntegrityDigest
	dispatchIndex              uint32
	params, results, codeBytes int
	owned                      bool
}

func NativeTailHostCertificateBytes() int { return int(unsafe.Sizeof(NativeTailHostCertificate{})) }

// NativeTailHostThunkWithCertificate keeps existing wrapper bytes unchanged.
// Proper-tail records currently provide the fixed64-cell host exchange only.
func NativeTailHostThunkWithCertificate(index uint32, params, results int, owned bool) ([]byte, *NativeTailHostCertificate, error) {
	if params < 0 || params > 64 || results < 0 || results > 64 {
		return nil, nil, unsupported("native tail host fixed exchange capacity")
	}
	code := hostIndirectSyncThunk(index, params, results, !owned)
	if len(code) == 0 {
		return nil, nil, unsupported("native tail host wrapper construction")
	}
	return code, &NativeTailHostCertificate{digest: codeimage.Digest(code), dispatchIndex: index, params: params, results: results, codeBytes: len(code), owned: owned}, nil
}

func (c *NativeTailHostCertificate) CodeBytes() int {
	if c == nil {
		return 0
	}
	return c.codeBytes
}

// Check the physical image lease before reading its exact logical span. Warm
// validation hashes the bounded immutable bytes without regenerating a thunk.
func (c *NativeTailHostCertificate) Validate(image *NativeTailCodeImage, offset int, index uint32, params, results int, owned bool) error {
	if c == nil || image == nil || c.codeBytes <= 0 || c.dispatchIndex != index || c.params != params || c.results != results || c.owned != owned {
		return unsupported("native tail host certificate identity")
	}
	if err := image.Validate(); err != nil {
		return err
	}
	b := image.Bytes()
	if offset < 0 || offset&15 != 0 || offset > len(b) || c.codeBytes > len(b)-offset || codeimage.Digest(b[offset:offset+c.codeBytes]) != c.digest {
		return unsupported("native tail host certificate image")
	}
	return nil
}

// NativeTailHostFunction is a frozen scalar host target, separate from a
// genuine framed guest definition. It cannot authorize a native guest frame.
// Dynamic owner snapshots validate this certificate without granting a guest frame.
type NativeTailHostFunction struct {
	image             *NativeTailCodeImage
	certificate       *NativeTailHostCertificate
	signature         *wasm.CompType
	job               *coreruntime.JobMemory
	context, anchor   []byte
	offset            int
	dispatchIndex     uint32
	owned             bool
	referenceCarriers bool
	collectorCarriers bool
}

// Charge both the staging signature and its frozen copy before construction.
// The image and certificate have their own lifetime/metadata charges.
func NativeTailHostFunctionBytes(values int) (int, error) {
	if values < 0 || values > 128 {
		return 0, unsupported("native tail host signature count")
	}
	return int(unsafe.Sizeof(NativeTailHostFunction{})) + 2*int(unsafe.Sizeof(wasm.CompType{})) + 2*values*int(unsafe.Sizeof(wasm.ValType{})), nil
}

// NewNativeTailHostFunction requires an image owner that authenticates the same
// physical instance as job/context/anchor, including its retained host binding
// and ordered signature. A shared cache lease alone cannot satisfy this contract:
// identical wrapper bytes do not authenticate another instance's callback or data.
func NewNativeTailHostFunction(image *NativeTailCodeImage, certificate *NativeTailHostCertificate, job *coreruntime.JobMemory, context, anchor []byte, offset int, dispatchIndex uint32, owned bool, signature wasm.CompType) (*NativeTailHostFunction, error) {
	return newNativeTailHostFunction(image, certificate, job, context, anchor, offset, dispatchIndex, owned, signature, false, false)
}

// NewNativeTailHostReferenceFunction is restricted to whole-module profiles
// that separately prove authenticated public reference-carrier transport. The
// ordinary constructor deliberately remains scalar-only.
func NewNativeTailHostReferenceFunction(image *NativeTailCodeImage, certificate *NativeTailHostCertificate, job *coreruntime.JobMemory, context, anchor []byte, offset int, dispatchIndex uint32, owned bool, signature wasm.CompType) (*NativeTailHostFunction, error) {
	return newNativeTailHostFunction(image, certificate, job, context, anchor, offset, dispatchIndex, owned, signature, true, false)
}

// NewNativeTailHostCollectorFunction requires a runtime-owned host binding in
// the caller's exact collector domain, with authenticated native root maps.
// The image owner must repeat that proof whenever the target is validated.
func NewNativeTailHostCollectorFunction(image *NativeTailCodeImage, certificate *NativeTailHostCertificate, job *coreruntime.JobMemory, context, anchor []byte, offset int, dispatchIndex uint32, owned bool, signature wasm.CompType) (*NativeTailHostFunction, error) {
	if !owned {
		return nil, unsupported("native tail collector host ownership")
	}
	return newNativeTailHostFunction(image, certificate, job, context, anchor, offset, dispatchIndex, owned, signature, false, true)
}

func newNativeTailHostFunction(image *NativeTailCodeImage, certificate *NativeTailHostCertificate, job *coreruntime.JobMemory, context, anchor []byte, offset int, dispatchIndex uint32, owned bool, signature wasm.CompType, referenceCarriers, collectorCarriers bool) (*NativeTailHostFunction, error) {
	f := NativeTailHostFunction{image: image, certificate: certificate, signature: &signature, job: job, context: context, anchor: anchor, offset: offset, dispatchIndex: dispatchIndex, owned: owned, referenceCarriers: referenceCarriers, collectorCarriers: collectorCarriers}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	f.signature = nativeTailCloneSignature(signature)
	return &f, nil
}

func (f *NativeTailHostFunction) Validate() error {
	if f == nil || f.signature == nil || f.signature.Kind != wasm.CompFunc || swarSignatureSlots(f.signature.Params) > 64 || swarSignatureSlots(f.signature.Results) > 64 {
		return unsupported("native tail host scalar signature extent")
	}
	for _, values := range [][]wasm.ValType{f.signature.Params, f.signature.Results} {
		for _, typ := range values {
			if !wasm.EqualValType(typ, wasm.I32) && !wasm.EqualValType(typ, wasm.I64) && !wasm.EqualValType(typ, wasm.F32) && !wasm.EqualValType(typ, wasm.F64) && !(f.collectorCarriers && nativeTailCollectorHostCarrier(typ)) && (!f.referenceCarriers || !wasm.EqualValType(typ, wasm.FuncRef) && !wasm.EqualValType(typ, wasm.ExternRef)) {
				return unsupported("native tail host scalar carrier")
			}
		}
	}
	if err := f.certificate.Validate(f.image, f.offset, f.dispatchIndex, swarSignatureSlots(f.signature.Params), swarSignatureSlots(f.signature.Results), f.owned); err != nil {
		return err
	}
	// The authenticated image owner has now confirmed this particular physical
	// lifetime. Only then inspect the borrowed context, job and canonical anchor.
	if f.job == nil || f.job.LinMemBase() == 0 || f.job.LinMemBase()&7 != 0 || len(f.context) != abi.RV64InstanceContextBytes || tailRAPointer(f.context)&7 != 0 || len(f.anchor) < 40 || tailRAPointer(f.anchor)&7 != 0 || binary.LittleEndian.Uint64(f.context[16:]) != uint64(tailRAPointer(f.anchor)) || binary.LittleEndian.Uint64(f.anchor[32:]) != uint64(tailRAPointer(f.context)) || binary.LittleEndian.Uint64(f.anchor[16:]) != uint64(f.job.LinMemBase()) {
		return unsupported("native tail host physical owner identity")
	}
	return nil
}

// The descriptor view carries no guest-frame claim. Only the separate host
// certificate may validate this entry in an explicitly marked dynamic row.
func (f *NativeTailHostFunction) function(signature *wasm.CompType) tailRAFunction {
	return tailRAFunction{published: f.image, snapshot: f.image.Bytes(), signature: signature, job: f.job, context: f.context, directory: f.anchor,
		info: FunctionInfo{Entry: f.offset, CodeBytes: f.certificate.CodeBytes(), Params: swarSignatureSlots(signature.Params), Results: swarSignatureSlots(signature.Results), mode: emissionMode{production: true, interruptible: true, invocationContext: true}}}
}

func nativeTailCollectorHostCarrier(typ wasm.ValType) bool {
	if wasm.EqualValType(typ, wasm.V128) {
		return true
	}
	if typ.Kind() != wasm.ValRef {
		return false
	}
	heap := typ.Ref().Heap()
	return heap.Kind() == wasm.HeapAbs && (heap.Abs() == wasm.HeapAny || heap.Abs() == wasm.HeapI31)
}
