//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// NativeTailFunction retains one genuine framed definition. A producer need
// only have its context anchor, not an indexed descriptor arena. The runtime
// must hold that particular producer's physical lease for every use.
type NativeTailFunction struct{ function tailRAFunction }

func NativeTailFunctionBytes(values int) (int, error) {
	if values < 0 || values > 512 {
		return 0, unsupported("native tail function signature count")
	}
	return int(unsafe.Sizeof(NativeTailFunction{})) + 2*int(unsafe.Sizeof(wasm.CompType{})) + 2*values*int(unsafe.Sizeof(wasm.ValType{})), nil
}

func NewNativeTailFunction(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, anchor []byte, entry, end int, functionIndex uint32, signature wasm.CompType) (NativeTailFunction, error) {
	f, err := nativeTailPublishedFunction(image, job, context, anchor, entry, end, functionIndex, &signature)
	if err != nil {
		return NativeTailFunction{}, err
	}
	f.signature = nativeTailCloneSignature(signature)
	return NativeTailFunction{function: f}, nil
}

// Validate the physical image before reading the borrowed job/context/anchor.
// Runtime metadata supplies exact entry boundaries; the image supplies framing.
func nativeTailPublishedFunction(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, anchor []byte, entry, end int, functionIndex uint32, signature *wasm.CompType) (tailRAFunction, error) {
	if image == nil || job == nil || len(context) != abi.RV64InstanceContextBytes || len(anchor) != 0 && len(anchor) < 40 {
		return tailRAFunction{}, unsupported("native tail function owner extent")
	}
	if err := image.Validate(); err != nil {
		return tailRAFunction{}, err
	}
	code := image.Bytes()
	if entry < 0 || entry&15 != 0 || end <= entry || end > len(code) || end&3 != 0 {
		return tailRAFunction{}, unsupported("native tail function span")
	}
	if err := tailRASignatureMode(signature, true); err != nil {
		return tailRAFunction{}, err
	}
	frame, err := NativeFrameBytes(code[:end], entry)
	if err != nil {
		return tailRAFunction{}, err
	}
	f := tailRAFunction{published: image, snapshot: code, job: job, context: context, directory: anchor, signature: signature,
		info: FunctionInfo{Entry: entry, CodeBytes: end - entry, FrameBytes: frame, FrameHeaderBytes: callFrameHeader, Params: swarSignatureSlots(signature.Params), Results: swarSignatureSlots(signature.Results), mode: emissionMode{production: true, interruptible: true, invocationContext: true, functionIndex: functionIndex}}}
	if err := f.validate(true); err != nil {
		return tailRAFunction{}, err
	}
	if len(anchor) != 0 && binary.LittleEndian.Uint64(anchor[16:]) != uint64(job.LinMemBase()) {
		return tailRAFunction{}, unsupported("native tail function home anchor")
	}
	return f, nil
}

func nativeTailCloneSignature(sig wasm.CompType) *wasm.CompType {
	return &wasm.CompType{Kind: wasm.CompFunc, Params: append([]wasm.ValType(nil), sig.Params...), Results: append([]wasm.ValType(nil), sig.Results...)}
}

// NativeTailImport describes an importer-owned ordinary bridge issued by the
// certified builder. Its position is its exact function import index. The
// A native target must be a genuine definition, never a reexported import
// bridge. A scalar host import instead carries only its Host certificate.
type NativeTailImport struct {
	Image              *NativeTailCodeImage
	Offset, CodeBytes  int
	Target             NativeTailFunction
	Arguments, Results []byte
	Certificate        *NativeImportCertificate
	Host               *NativeTailHostFunction
}

// Charge construction inputs, source/target arrays and frozen signatures before
// converting runtime types. Producer functions/images are charged separately;
// certificates belong to the already charged importer lifetime.
func NativeTailImportedOwnerBytes(count, imports, values int) (int, error) {
	if count <= 0 || count > MaxFunctions || imports <= 0 || imports > count || imports > MaxHostFunctionImports || values < 0 || values > 512*count {
		return 0, unsupported("native imported tail owner metadata count")
	}
	size := int(unsafe.Sizeof(NativeTailOwner{})) + 2*int(unsafe.Sizeof(NativeTailCodeImage{})) + count*(2*int(unsafe.Sizeof(tailRAFunction{}))+3*int(unsafe.Sizeof(wasm.CompType{}))+8+int(unsafe.Sizeof((*tailRADynamicProxy)(nil)))) + imports*(int(unsafe.Sizeof(NativeTailImport{}))+int(unsafe.Sizeof(tailRADynamicProxy{}))) + 3*values*int(unsafe.Sizeof(wasm.ValType{}))
	if size > MaxModuleBytes {
		return 0, unsupported("native imported tail owner metadata bound")
	}
	return size, nil
}

// NativeTailHostOwnerBytes adds the optional immutable host-target directory.
// Individual host targets, images and certificates are charged separately.
func NativeTailHostOwnerBytes(count, imports, values int) (int, error) {
	size, err := NativeTailImportedOwnerBytes(count, imports, values)
	if err != nil {
		return 0, err
	}
	size += count * int(unsafe.Sizeof((*NativeTailHostFunction)(nil)))
	if size > MaxModuleBytes {
		return 0, unsupported("native host owner metadata bound")
	}
	return size, nil
}

// NewNativeTailImportedOwner preserves canonical wrapper rows while selecting
// certified native definitions or host wrappers. Entries contain only definitions;
// signatures include imports first, in the full Wasm function namespace.
// An import-only owner has no guest image or entries. Its certified import images
// authenticate the physical owner before any borrowed context is inspected.
func NewNativeTailImportedOwner(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, descriptors, directory []byte, entries []int, signatures []wasm.CompType, imports []NativeTailImport, trampoline uintptr) (*NativeTailOwner, error) {
	return newNativeTailImportedOwner(image, job, context, descriptors, directory, entries, len(signatures), func(i int) *wasm.CompType { return &signatures[i] }, imports, trampoline, false)
}

// NewNativeTailSparseImportedOwner retains the full imported+defined namespace.
// Only a defined signature may be nil, marking an internal physical frame with
// no callable descriptor identity. Every imported signature remains numeric.
func NewNativeTailSparseImportedOwner(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, descriptors, directory []byte, entries []int, signatures []*wasm.CompType, imports []NativeTailImport, trampoline uintptr) (*NativeTailOwner, error) {
	return newNativeTailImportedOwner(image, job, context, descriptors, directory, entries, len(signatures), func(i int) *wasm.CompType { return signatures[i] }, imports, trampoline, true)
}

func NativeTailSparseImportedOwnerBytes(count, imports, nonCallable, values int) (int, error) {
	return nativeTailSparseImportedOwnerBytes(count, imports, nonCallable, values, false)
}

func NativeTailSparseHostOwnerBytes(count, imports, nonCallable, values int) (int, error) {
	return nativeTailSparseImportedOwnerBytes(count, imports, nonCallable, values, true)
}

// Full source/target arrays remain charged. Add pointer staging and the retained
// mask; only callable signatures consume type storage. Hosts retain a full
// optional pointer directory, including nil entries at every internal hole.
func nativeTailSparseImportedOwnerBytes(count, imports, nonCallable, values int, hosts bool) (int, error) {
	if count <= 0 || count > MaxFunctions || imports <= 0 || imports > count || nonCallable < 0 || nonCallable > count-imports || values < 0 || values > 512*(count-nonCallable) {
		return 0, unsupported("native sparse imported owner counts")
	}
	size, err := NativeTailImportedOwnerBytes(count, imports, values)
	if err != nil {
		return 0, err
	}
	size += count * (int(unsafe.Sizeof((*wasm.CompType)(nil))) + int(unsafe.Sizeof(false)))
	if hosts {
		size += count * int(unsafe.Sizeof((*NativeTailHostFunction)(nil)))
	}
	if size > MaxModuleBytes {
		return 0, unsupported("native sparse imported owner metadata bound")
	}
	return size, nil
}

func newNativeTailImportedOwner(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, descriptors, directory []byte, entries []int, n int, signature func(int) *wasm.CompType, imports []NativeTailImport, trampoline uintptr, sparse bool) (*NativeTailOwner, error) {
	ni := len(imports)
	if _, err := NativeTailImportedOwnerBytes(n, ni, 0); err != nil {
		return nil, err
	}
	if len(entries) < n-ni || len(entries) > 2*MaxFunctions || len(context) != abi.RV64InstanceContextBytes || len(descriptors) != 40*(n+1) || len(directory) != abi.RV64TailTargetsHeaderBytes+abi.RV64TailTargetsRowBytes*n || job == nil || trampoline == 0 || trampoline&3 != 0 {
		return nil, unsupported("native imported tail owner extent")
	}
	if n == ni {
		if image != nil || len(entries) != 0 {
			return nil, unsupported("import-only tail owner has a guest image")
		}
	} else {
		if err := image.Validate(); err != nil {
			return nil, err
		}
	}
	// No retained allocation or native publication precedes all count, image,
	// signature and exact bridge provenance checks.
	hasHosts := false
	for i := range imports {
		hasHosts = hasHosts || imports[i].Host != nil
	}
	budget := NativeTailImportedOwnerBytes
	if hasHosts {
		budget = NativeTailHostOwnerBytes
	}
	values, nonCallable := 0, 0
	for i := 0; i < n; i++ {
		sig := signature(i)
		if sparse && sig == nil && i >= ni {
			nonCallable++
			continue
		}
		if err := tailRASignature(sig); err != nil {
			return nil, err
		}
		if len(sig.Fields) != 0 || sig.Array != (wasm.FieldType{}) {
			return nil, unsupported("native imported inactive signature metadata")
		}
		values += len(sig.Params) + len(sig.Results)
	}
	if sparse {
		if _, err := nativeTailSparseImportedOwnerBytes(n, ni, nonCallable, values, hasHosts); err != nil {
			return nil, err
		}
	} else if _, err := budget(n, ni, values); err != nil {
		return nil, err
	}
	for i := 0; i < n; i++ {
		sig := signature(i)
		if i < ni {
			p := &imports[i]
			if p.Host != nil {
				h := p.Host
				if p.Certificate != nil || p.Target.function.signature != nil || p.Arguments != nil || p.Results != nil || p.Image != h.image || p.Offset != h.offset || p.CodeBytes != h.certificate.CodeBytes() {
					return nil, unsupported("native host import certificate shape")
				}
				if err := h.Validate(); err != nil {
					return nil, err
				}
				if h.job != job || tailRAPointer(h.context) != tailRAPointer(context) || tailRAPointer(h.anchor) != tailRAPointer(descriptors) || !wasm.FuncTypeEqual(sig, h.signature) {
					return nil, unsupported("native host import owner/signature")
				}
				continue
			}
			if p.Image == nil || p.Certificate == nil || p.CodeBytes != p.Certificate.CodeBytes() {
				return nil, unsupported("native imported tail owner certificate")
			}
			if err := p.Target.function.validate(true); err != nil {
				return nil, err
			}
			if !wasm.FuncTypeEqual(sig, p.Target.function.signature) {
				return nil, unsupported("native imported tail owner ordered signature")
			}
			f := tailRAFunction{published: p.Image, snapshot: p.Image.Bytes(), job: job, context: context, directory: descriptors, signature: sig, info: FunctionInfo{Entry: p.Offset, CodeBytes: p.CodeBytes}}
			proxy := tailRADynamicProxy{certificate: p.Certificate, importIndex: uint32(i), arguments: p.Arguments, results: p.Results}
			if err := proxy.validate(f, p.Target.function, trampoline); err != nil {
				return nil, err
			}
		} else {
			local, end := i-ni, len(image.Bytes())
			if local+1 < len(entries) {
				end = entries[local+1]
			}
			if _, err := nativeTailImportedLocal(image, job, context, descriptors, entries[local], end, uint32(i), sig); err != nil {
				return nil, err
			}
		}
	}
	o := &NativeTailOwner{trampoline: trampoline, owner: tailRADynamicOwner{descriptors: descriptors, context: context, directory: directory, functions: make([]tailRAFunction, n), targets: make([]tailRAFunction, n), proxies: make([]*tailRADynamicProxy, n), signatureKeys: make([]uint64, n)}}
	if hasHosts {
		o.owner.hosts = make([]*NativeTailHostFunction, n)
	}
	if sparse {
		o.owner.uncallable = make([]bool, n)
	}
	for i := 0; i < n; i++ {
		var frozen *wasm.CompType
		if sig := signature(i); sig != nil {
			frozen = nativeTailCloneSignature(*sig)
			keyImage := image
			if keyImage == nil && i < ni {
				keyImage = imports[i].Image
			}
			key, err := keyImage.functionKey(uint32(i), frozen)
			if err != nil {
				return nil, err
			}
			o.owner.signatureKeys[i] = key
		} else {
			o.owner.uncallable[i] = true
		}
		if i < ni {
			p := &imports[i]
			if p.Host != nil {
				f := p.Host.function(frozen)
				o.owner.functions[i], o.owner.targets[i], o.owner.hosts[i] = f, f, p.Host
				continue
			}
			o.owner.functions[i] = tailRAFunction{published: p.Image, snapshot: p.Image.Bytes(), job: job, context: context, directory: descriptors, signature: frozen, info: FunctionInfo{Entry: p.Offset, CodeBytes: p.CodeBytes}}
			o.owner.targets[i] = p.Target.function
			o.owner.proxies[i] = &tailRADynamicProxy{certificate: p.Certificate, importIndex: uint32(i), arguments: p.Arguments, results: p.Results}
		} else {
			local, end := i-ni, len(image.Bytes())
			if local+1 < len(entries) {
				end = entries[local+1]
			}
			f, err := nativeTailImportedLocal(image, job, context, descriptors, entries[local], end, uint32(i), frozen)
			if err != nil {
				return nil, err
			}
			o.owner.functions[i], o.owner.targets[i] = f, f
		}
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	return o, nil
}

// Internal local frames carry no invented numeric signature. Their real image,
// span and frame remain authenticated just like their callable neighbors.
func nativeTailImportedLocal(image *NativeTailCodeImage, job *coreruntime.JobMemory, context, anchor []byte, entry, end int, index uint32, signature *wasm.CompType) (tailRAFunction, error) {
	if signature != nil {
		return nativeTailPublishedFunction(image, job, context, anchor, entry, end, index, signature)
	}
	if image == nil || job == nil || len(context) != abi.RV64InstanceContextBytes || len(anchor) < 40 {
		return tailRAFunction{}, unsupported("native internal imported-owner extent")
	}
	if err := image.Validate(); err != nil {
		return tailRAFunction{}, err
	}
	code := image.Bytes()
	if entry < 0 || entry&15 != 0 || end <= entry || end > len(code) || end&3 != 0 {
		return tailRAFunction{}, unsupported("native internal imported-owner span")
	}
	frame, err := NativeFrameBytes(code[:end], entry)
	if err != nil {
		return tailRAFunction{}, err
	}
	f := tailRAFunction{published: image, snapshot: code, job: job, context: context, directory: anchor, info: FunctionInfo{Entry: entry, CodeBytes: end - entry, FrameBytes: frame, FrameHeaderBytes: callFrameHeader, mode: emissionMode{production: true, interruptible: true, invocationContext: true, functionIndex: index}}}
	if err := f.validateFrame(true); err != nil {
		return tailRAFunction{}, err
	}
	if binary.LittleEndian.Uint64(anchor[16:]) != uint64(job.LinMemBase()) {
		return tailRAFunction{}, unsupported("native internal imported-owner home")
	}
	return f, nil
}
