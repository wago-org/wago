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
	if image == nil || job == nil || len(context) != abi.RV64InstanceContextBytes || len(anchor) < 40 {
		return tailRAFunction{}, unsupported("native tail function owner extent")
	}
	if err := image.Validate(); err != nil {
		return tailRAFunction{}, err
	}
	code := image.Bytes()
	if entry < 0 || entry&15 != 0 || end <= entry || end > len(code) || end&3 != 0 {
		return tailRAFunction{}, unsupported("native tail function span")
	}
	if err := tailRASignature(signature); err != nil {
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
	if binary.LittleEndian.Uint64(anchor[16:]) != uint64(job.LinMemBase()) {
		return tailRAFunction{}, unsupported("native tail function home anchor")
	}
	return f, nil
}

func nativeTailCloneSignature(sig wasm.CompType) *wasm.CompType {
	sig.Params = append([]wasm.ValType(nil), sig.Params...)
	sig.Results = append([]wasm.ValType(nil), sig.Results...)
	return &sig
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
	n, ni := len(signatures), len(imports)
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
	values := 0
	for i := range signatures {
		sig := &signatures[i]
		if err := tailRASignature(sig); err != nil {
			return nil, err
		}
		values += len(sig.Params) + len(sig.Results)
		if _, err := budget(n, ni, values); err != nil {
			return nil, err
		}
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
			if _, err := nativeTailPublishedFunction(image, job, context, descriptors, entries[local], end, uint32(i), sig); err != nil {
				return nil, err
			}
		}
	}
	o := &NativeTailOwner{trampoline: trampoline, owner: tailRADynamicOwner{descriptors: descriptors, context: context, directory: directory, functions: make([]tailRAFunction, n), targets: make([]tailRAFunction, n), proxies: make([]*tailRADynamicProxy, n), signatureKeys: make([]uint64, n)}}
	if hasHosts {
		o.owner.hosts = make([]*NativeTailHostFunction, n)
	}
	for i, sig := range signatures {
		frozen := nativeTailCloneSignature(sig)
		o.owner.signatureKeys[i] = wasm.StructuralFuncTypeKey(frozen)
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
			f, err := nativeTailPublishedFunction(image, job, context, descriptors, entries[local], end, uint32(i), frozen)
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
