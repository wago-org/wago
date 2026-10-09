//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Private dynamic proof. All mappings are exclusively retained by
// the invocation owner through execution and parks. Canonical imported proxies
// preserve ordinary bridge identity while selecting true defined framed targets.
// Scalar host rows require a separate immutable host construction certificate.
type tailRADynamicOwner struct {
	descriptors, context, directory []byte
	functions                       []tailRAFunction
	// Only the opaque runtime constructor supplies keys, after cloning and
	// validating signatures. Private mutable proof inputs leave this nil.
	signatureKeys []uint64
	// Nil retains the legacy all-callable contract. Sparse owners keep
	// every physical ordinal but expose no descriptor or target for true bits.
	uncallable []bool
	// Optional parallel producer/proxy metadata. Nil targets selects the source
	// defined functions. A nonnil proxy authenticates the exact ordinary bridge.
	targets []tailRAFunction
	proxies []*tailRADynamicProxy
	hosts   []*NativeTailHostFunction
}

type tailRADynamicProxy struct {
	certificate        *NativeImportCertificate
	importIndex        uint32
	arguments, results []byte
}

func (o tailRADynamicOwner) target(i int) tailRAFunction {
	if o.targets != nil {
		return o.targets[i]
	}
	return o.functions[i]
}

func (o tailRADynamicOwner) retainedFunction(i int) tailRAFunction {
	if i < len(o.functions) {
		return o.functions[i]
	}
	return o.targets[i-len(o.functions)]
}

func (o tailRADynamicOwner) proxy(i int) *tailRADynamicProxy {
	if o.proxies == nil {
		return nil
	}
	return o.proxies[i]
}

func (o tailRADynamicOwner) host(i int) *NativeTailHostFunction {
	if o.hosts == nil {
		return nil
	}
	return o.hosts[i]
}

func (o tailRADynamicOwner) targetFlags(i int) uint64 {
	if o.host(i) != nil {
		return abi.RV64TailTargetsHost
	}
	return 0
}

type tailRADynamicSite struct {
	referenceBase int
	key           uint64
	owners        []tailRADynamicOwner
}

// Charge the actual copied Go metadata and both emitted target checks before
// copying, emitting, mapping or binding a capability. Native payload slices
// remain borrowed from the physical owners, with no per-hop roots/allocation.
func (d *tailRADynamicSite) budget() (metadata, code int, err error) {
	if d == nil {
		return 0, 0, nil
	}
	if len(d.owners) == 0 || len(d.owners) > tailRAMaxRanges {
		return 0, 0, unsupported("dynamic tail owner count")
	}
	metadata = int(unsafe.Sizeof(*d)) + len(d.owners)*int(unsafe.Sizeof(tailRADynamicOwner{}))
	code = 2048 + 1024*len(d.owners)
	count := 0
	for _, o := range d.owners {
		if len(o.functions) == 0 || len(o.functions) > MaxFunctions-count || o.signatureKeys != nil && len(o.signatureKeys) != len(o.functions) || o.targets != nil && len(o.targets) != len(o.functions) || o.proxies != nil && (len(o.proxies) != len(o.functions) || o.targets == nil) || o.hosts != nil && len(o.hosts) != len(o.functions) || len(o.descriptors) != 40*(len(o.functions)+1) || len(o.context) != abi.RV64InstanceContextBytes || len(o.directory) != abi.RV64TailTargetsHeaderBytes+abi.RV64TailTargetsRowBytes*len(o.functions) {
			return 0, 0, unsupported("dynamic tail retained owner extent")
		}
		if o.uncallable != nil && (len(o.uncallable) != len(o.functions) || o.signatureKeys == nil || o.hosts != nil && o.targets == nil) {
			return 0, 0, unsupported("dynamic tail sparse owner extent")
		}
		count += len(o.functions)
		metadata += (len(o.functions)+len(o.targets))*int(unsafe.Sizeof(tailRAFunction{})) + len(o.proxies)*int(unsafe.Sizeof((*tailRADynamicProxy)(nil))) + len(o.signatureKeys)*8 + len(o.hosts)*int(unsafe.Sizeof((*NativeTailHostFunction)(nil)))
		metadata += len(o.uncallable) * int(unsafe.Sizeof(false))
		for _, p := range o.proxies {
			if p != nil {
				metadata += int(unsafe.Sizeof(*p))
			}
		}
		for i := 0; i < len(o.functions)+len(o.targets); i++ {
			f := o.retainedFunction(i)
			ordinal := i
			if ordinal >= len(o.functions) {
				ordinal -= len(o.functions)
			}
			if o.uncallable != nil && o.uncallable[ordinal] {
				if f.signature != nil || o.signatureKeys[ordinal] != 0 || f.info.Params != 0 || f.info.Results != 0 || f.published == nil || o.proxy(ordinal) != nil || o.host(ordinal) != nil {
					return 0, 0, unsupported("dynamic tail noncallable signature metadata")
				}
				if i >= len(o.functions) && !sameNativeTailPhysicalFunction(f, o.functions[ordinal]) {
					return 0, 0, unsupported("dynamic tail noncallable target differs from source")
				}
				continue
			}
			if f.signature == nil || len(f.signature.Params) > 256 || len(f.signature.Results) > 256 || len(f.signature.Fields) != 0 || f.signature.Array != (wasm.FieldType{}) {
				return 0, 0, unsupported("dynamic tail frozen signature extent")
			}
			metadata += int(unsafe.Sizeof(wasm.CompType{})) + (len(f.signature.Params)+len(f.signature.Results))*int(unsafe.Sizeof(wasm.ValType{}))
		}
		code += 1024 * len(o.functions)
		if metadata > MaxModuleBytes || code > MaxCodeBytes {
			return 0, 0, unsupported("dynamic tail metadata/code budget")
		}
	}
	return metadata, code, nil
}

// Hole targets mirror their physical source exactly. Comparing identities and
// extents here does not dereference borrowed native storage; Validate checks
// every particular published lease before reading any context or live row.
func sameNativeTailPhysicalFunction(a, b tailRAFunction) bool {
	return a.code == b.code && a.published == b.published && a.info == b.info && a.signature == b.signature && a.job == b.job &&
		len(a.snapshot) == len(b.snapshot) && tailRAPointer(a.snapshot) == tailRAPointer(b.snapshot) &&
		len(a.context) == len(b.context) && tailRAPointer(a.context) == tailRAPointer(b.context) &&
		len(a.directory) == len(b.directory) && tailRAPointer(a.directory) == tailRAPointer(b.directory)
}

func (d *tailRADynamicSite) validate(s *tailRAProofSite) error {
	if _, _, err := d.budget(); err != nil {
		return err
	}
	if !s.invocationContext || s.hostEntry || d.referenceBase < callFrameHeader || d.referenceBase > s.frameBytes-8 || d.referenceBase&7 != 0 || d.key != wasm.StructuralFuncTypeKey(&wasm.CompType{Kind: wasm.CompFunc, Params: s.params, Results: s.results}) {
		return unsupported("dynamic tail exact signature/reference home")
	}
	for n := range s.params {
		// A SWAR scalar's zero upper half is part of its canonical home too.
		base, width := s.sourceBase+n*s.sourceStride, s.sourceStride
		if d.referenceBase < base+width && base < d.referenceBase+8 {
			return unsupported("dynamic tail reference overlaps argument home")
		}
	}
	for n, o := range d.owners {
		anchor := tailRAPointer(o.descriptors)
		for _, prev := range d.owners[:n] {
			p := tailRAPointer(prev.descriptors)
			if anchor < p+uintptr(len(prev.descriptors)) && p < anchor+uintptr(len(o.descriptors)) || tailRAPointer(prev.context) == tailRAPointer(o.context) {
				return unsupported("dynamic tail ambiguous retained owner")
			}
		}
		if err := o.validate(s.target.hostTrampoline); err != nil {
			return err
		}
	}
	return nil
}

// budget validates all slice extents before this owner authentication runs.
func (o tailRADynamicOwner) validate(trampoline uintptr) error {
	// A published image's runtime owner first confirms the particular instance
	// lease. Its context and descriptor slices may already be physically retired
	// if a caller tries to reuse a stale snapshot after releasing that lease.
	for i := 0; i < len(o.functions)+len(o.targets); i++ {
		if err := o.retainedFunction(i).validateCode(); err != nil {
			return err
		}
	}
	anchor, ctx, dir := tailRAPointer(o.descriptors), tailRAPointer(o.context), tailRAPointer(o.directory)
	if !(tailRARange{anchor, uintptr(len(o.descriptors))}).valid(8) || !(tailRARange{dir, uintptr(len(o.directory))}).valid(8) || ctx == 0 || ctx&7 != 0 || binary.LittleEndian.Uint64(o.context[16:]) != uint64(anchor) || binary.LittleEndian.Uint64(o.descriptors[32:]) != uint64(ctx) || binary.LittleEndian.Uint64(o.descriptors[8:]) != uint64(dir) || binary.LittleEndian.Uint64(o.directory) != abi.RV64TailTargetsMagic || binary.LittleEndian.Uint32(o.directory[8:]) != abi.RV64TailTargetsVersion || binary.LittleEndian.Uint32(o.directory[12:]) != uint32(len(o.functions)) || binary.LittleEndian.Uint64(o.directory[16:]) != uint64(anchor) || binary.LittleEndian.Uint64(o.directory[24:]) != uint64(ctx) {
		return unsupported("dynamic tail retained directory identity")
	}
	for i, f := range o.functions {
		if o.uncallable != nil && o.uncallable[i] {
			if err := f.validateFrame(true); err != nil {
				return err
			}
			var emptyDescriptor [40]byte
			var emptyTarget [abi.RV64TailTargetsRowBytes]byte
			desc := o.descriptors[40*(i+1) : 40*(i+2)]
			row := o.directory[abi.RV64TailTargetsHeaderBytes+i*abi.RV64TailTargetsRowBytes : abi.RV64TailTargetsHeaderBytes+(i+1)*abi.RV64TailTargetsRowBytes]
			if !f.info.mode.invocationContext || tailRAPointer(f.context) != ctx || tailRAPointer(f.directory) != anchor || binary.LittleEndian.Uint64(o.descriptors[16:]) != uint64(f.job.LinMemBase()) || !bytes.Equal(desc, emptyDescriptor[:]) || !bytes.Equal(row, emptyTarget[:]) {
				return unsupported("dynamic tail noncallable physical owner or row")
			}
			continue
		}
		t := o.target(i)
		if len(t.context) != abi.RV64InstanceContextBytes {
			return unsupported("dynamic tail true producer context extent")
		}
		if h := o.host(i); h != nil {
			if o.proxy(i) != nil {
				return unsupported("host target cannot use a native proxy")
			}
			if err := h.Validate(); err != nil {
				return err
			}
			if !wasm.FuncTypeEqual(t.signature, h.signature) || t.published != h.image || t.info.Entry != h.offset || t.info.CodeBytes != h.certificate.CodeBytes() || t.job != h.job || tailRAPointer(t.context) != tailRAPointer(h.context) || tailRAPointer(t.directory) != tailRAPointer(h.anchor) || f.published != h.image || f.info.Entry != h.offset || f.info.CodeBytes != h.certificate.CodeBytes() {
				return unsupported("dynamic host target differs from certified owner")
			}
		} else if err := t.validate(true); err != nil {
			return err
		}
		if !t.info.mode.invocationContext || !wasm.FuncTypeEqual(f.signature, t.signature) || binary.LittleEndian.Uint64(t.directory[16:]) != uint64(t.job.LinMemBase()) {
			return unsupported("dynamic tail exact producer signature/anchor")
		}
		if p := o.proxy(i); p != nil {
			if err := p.validate(f, t, trampoline); err != nil {
				return err
			}
		} else {
			if o.host(i) == nil {
				if err := f.validate(true); err != nil {
					return err
				}
			}
			if f.codeBase()+uintptr(f.info.Entry) != t.codeBase()+uintptr(t.info.Entry) || f.job.LinMemBase() != t.job.LinMemBase() || tailRAPointer(f.context) != tailRAPointer(t.context) || tailRAPointer(f.directory) != tailRAPointer(t.directory) {
				return unsupported("dynamic tail defined descriptor retargeted")
			}
		}
		entry, home := f.codeBase()+uintptr(f.info.Entry), f.job.LinMemBase()
		var key uint64
		if o.signatureKeys != nil {
			key = o.signatureKeys[i]
		} else {
			key = wasm.StructuralFuncTypeKey(f.signature)
		}
		desc := o.descriptors[40*(i+1):]
		row := o.directory[abi.RV64TailTargetsHeaderBytes+i*abi.RV64TailTargetsRowBytes:]
		if o.proxy(i) == nil && !f.info.mode.invocationContext || tailRAPointer(f.context) != ctx || tailRAPointer(f.directory) != anchor || binary.LittleEndian.Uint64(o.descriptors[16:]) != uint64(home) || binary.LittleEndian.Uint64(desc) != uint64(entry) || binary.LittleEndian.Uint64(desc[8:]) != key || binary.LittleEndian.Uint64(desc[16:]) != uint64(home)|abi.FuncRefLocalWrapperHomeTag || binary.LittleEndian.Uint64(desc[24:]) != uint64(anchor+uintptr(40*(i+1))) || binary.LittleEndian.Uint64(desc[32:]) != uint64(ctx) || binary.LittleEndian.Uint64(row) != uint64(t.codeBase()+uintptr(t.info.Entry)) || binary.LittleEndian.Uint64(row[8:]) != uint64(t.job.LinMemBase()) || binary.LittleEndian.Uint64(row[16:]) != uint64(tailRAPointer(t.context)) || binary.LittleEndian.Uint64(row[24:]) != o.targetFlags(i) {
			return unsupported("dynamic tail genuine defined target")
		}
	}
	return nil
}

func (p *tailRADynamicProxy) validate(source, target tailRAFunction, trampoline uintptr) error {
	if err := source.validateCode(); err != nil {
		return err
	}
	if source.codeBase() == 0 || source.codeBase()&15 != 0 || source.job == nil || source.job.LinMemBase() == 0 || len(source.snapshot) == 0 || len(source.snapshot) > MaxCodeBytes || !bytes.Equal(source.codeBytes(), source.snapshot) || len(source.context) != abi.RV64InstanceContextBytes || tailRAPointer(source.context)&7 != 0 || len(source.directory) < 40 || tailRAPointer(source.directory)&7 != 0 || source.info.Entry < 0 || source.info.Entry&15 != 0 || source.info.Entry > len(source.snapshot) || source.info.CodeBytes <= 0 || source.info.CodeBytes > len(source.snapshot)-source.info.Entry {
		return unsupported("dynamic tail retained import proxy image")
	}
	callee := NativeImportTarget{Entry: target.codeBase() + uintptr(target.info.Entry), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), Arguments: p.arguments, Results: p.results, ArgumentSlots: swarSignatureSlots(source.signature.Params), ResultSlots: swarSignatureSlots(source.signature.Results), StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}
	if p.certificate != nil {
		if source.published == nil || source.info.CodeBytes != p.certificate.CodeBytes() {
			return unsupported("dynamic tail certified proxy extent")
		}
		return p.certificate.Validate(source.published, source.info.Entry, callee, source.job.LinMemBase(), tailRAPointer(source.context), p.importIndex)
	}
	expected, err := NativeImportThunk(callee, source.job.LinMemBase(), tailRAPointer(source.context), p.importIndex)
	if err != nil || !bytes.Equal(source.snapshot[source.info.Entry:source.info.Entry+source.info.CodeBytes], expected) {
		return unsupported("dynamic tail exact native import proxy builder")
	}
	return nil
}

func tailRADynamicFreezeFunctions(functions []tailRAFunction) []tailRAFunction {
	if functions == nil {
		return nil
	}
	frozen := append([]tailRAFunction(nil), functions...)
	for n := range frozen {
		if frozen[n].signature == nil {
			continue // Only explicitly masked rows pass the preceding budget check.
		}
		frozen[n].signature = nativeTailCloneSignature(*frozen[n].signature)
	}
	return frozen
}

func (d *tailRADynamicSite) freeze() *tailRADynamicSite {
	if d == nil {
		return nil
	}
	f := *d
	f.owners = append([]tailRADynamicOwner(nil), d.owners...)
	for i := range f.owners {
		f.owners[i].uncallable = append([]bool(nil), d.owners[i].uncallable...)
		f.owners[i].signatureKeys = append([]uint64(nil), d.owners[i].signatureKeys...)
		f.owners[i].functions = tailRADynamicFreezeFunctions(d.owners[i].functions)
		f.owners[i].targets = tailRADynamicFreezeFunctions(d.owners[i].targets)
		f.owners[i].hosts = append([]*NativeTailHostFunction(nil), d.owners[i].hosts...)
		if d.owners[i].proxies != nil {
			f.owners[i].proxies = append([]*tailRADynamicProxy(nil), d.owners[i].proxies...)
			for n, p := range f.owners[i].proxies {
				if p != nil {
					v := *p
					f.owners[i].proxies[n] = &v
				}
			}
		}
	}
	return &f
}

// The private proof's declared guest budget must include every distinct target
// image. Deduplicate with bounded metadata scans before allocating scratch maps.
func nativeTailDynamicCodeBytes(sites []NativeTailSite) (int, error) {
	bytes := 0
	for sn, s := range sites {
		if s.dynamic == nil {
			continue
		}
		for on, o := range s.dynamic.owners {
			for fn := 0; fn < len(o.functions)+len(o.targets); fn++ {
				f := o.retainedFunction(fn)
				seen := false
			previous:
				for pn, p := range sites[:sn+1] {
					if p.dynamic == nil {
						continue
					}
					for po, owner := range p.dynamic.owners {
						for pf := 0; pf < len(owner.functions)+len(owner.targets); pf++ {
							prev := owner.retainedFunction(pf)
							if pn == sn && po == on && pf == fn {
								break previous
							}
							if prev.codeKey() == f.codeKey() {
								seen = true
								break previous
							}
						}
					}
				}
				if !seen {
					if len(f.snapshot) > MaxCodeBytes-bytes {
						return 0, unsupported("dynamic tail retained code bound")
					}
					bytes += len(f.snapshot)
				}
			}
		}
	}
	return bytes, nil
}

// Membership is established against frozen owned slices before dereferencing
// the supplied reference. X16/17/18 retain entry/home/context through finalizing;
// X19/20/21 are owner cursors. X26/27 remain reserved for Go's CTXT/g registers.
func (e *tailRAEmitter) dynamicNativeTarget(d *tailRADynamicSite) {
	a := &e.a
	tailRAAddress(a, enc.X14, enc.SP, d.referenceBase)
	a.Ld(enc.X14, enc.X14, 0)
	var selected []int
	for _, o := range d.owners {
		anchor := tailRAPointer(o.descriptors)
		a.Constant64(enc.X19, int64(anchor+40))
		below := a.BranchLong(enc.LTU, enc.X14, enc.X19)
		a.Constant64(enc.X19, int64(anchor+uintptr(len(o.descriptors))-40))
		above := a.BranchLong(enc.LTU, enc.X19, enc.X14)
		a.Constant64(enc.X19, int64(anchor))
		a.Constant64(enc.X20, int64(tailRAPointer(o.context)))
		a.Constant64(enc.X21, int64(tailRAPointer(o.directory)))
		a.Constant64(enc.X22, int64(len(o.functions)))
		selected = append(selected, a.JumpLong(false))
		e.patch(below)
		e.patch(above)
	}
	e.fail(enc.EQ, enc.Zero, enc.Zero)
	for _, site := range selected {
		e.patch(site)
	}
	a.Sub(enc.X15, enc.X14, enc.X19)
	a.Addi(enc.Scratch0, enc.Zero, 40)
	a.Remu(enc.X29, enc.X15, enc.Scratch0)
	e.fail(enc.NE, enc.X29, enc.Zero)
	a.Divu(enc.X15, enc.X15, enc.Scratch0)
	a.Addi(enc.X15, enc.X15, -1)
	a.Ld(enc.X29, enc.X14, 24)
	e.fail(enc.NE, enc.X29, enc.X14)
	a.Ld(enc.X29, enc.X14, 8)
	a.Constant64(enc.Scratch0, int64(d.key))
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, enc.X14, 32)
	e.fail(enc.NE, enc.X29, enc.X20)
	a.Ld(enc.X29, enc.X19, 32)
	e.fail(enc.NE, enc.X29, enc.X20)
	a.Ld(enc.X29, enc.X20, 16)
	e.fail(enc.NE, enc.X29, enc.X19)
	a.Ld(enc.X29, enc.X19, 8)
	e.fail(enc.NE, enc.X29, enc.X21)
	a.Ld(enc.X29, enc.X21, 0)
	a.Constant64(enc.Scratch0, int64(abi.RV64TailTargetsMagic))
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Lwu(enc.X29, enc.X21, 8)
	a.Addi(enc.Scratch0, enc.Zero, abi.RV64TailTargetsVersion)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Lwu(enc.X29, enc.X21, 12)
	e.fail(enc.NE, enc.X29, enc.X22)
	e.fail(enc.GEU, enc.X15, enc.X29)
	a.Ld(enc.X29, enc.X21, 16)
	e.fail(enc.NE, enc.X29, enc.X19)
	a.Ld(enc.X29, enc.X21, 24)
	e.fail(enc.NE, enc.X29, enc.X20)
	a.Slli(enc.X15, enc.X15, 5)
	a.Addi(enc.X15, enc.X15, abi.RV64TailTargetsHeaderBytes)
	a.Add(enc.X15, enc.X21, enc.X15)
	a.Ld(enc.X16, enc.X15, 0)
	a.Ld(enc.X17, enc.X15, 8)
	a.Ld(enc.X18, enc.X15, 16)
	a.Ld(enc.X29, enc.X19, 16)
	a.Constant64(enc.Scratch0, int64(abi.FuncRefLocalWrapperHomeTag))
	a.Or(enc.X29, enc.X29, enc.Scratch0)
	a.Ld(enc.Scratch0, enc.X14, 16)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	// Authenticate the exact frozen entry as well as the live directory. Even
	// coordinated descriptor/row corruption cannot substitute arbitrary code.
	var genuine []int
	for _, o := range d.owners {
		for i, f := range o.functions {
			if o.uncallable != nil && o.uncallable[i] {
				continue
			}
			t := o.target(i)
			// A live key cannot relabel a genuine function of another signature.
			if wasm.StructuralFuncTypeKey(f.signature) != d.key || wasm.StructuralFuncTypeKey(t.signature) != d.key {
				continue
			}
			a.Constant64(enc.X29, int64(tailRAPointer(o.descriptors)+uintptr(40*(i+1))))
			next := a.BranchLong(enc.NE, enc.X14, enc.X29)
			a.Constant64(enc.X29, int64(f.codeBase()+uintptr(f.info.Entry)))
			a.Ld(enc.Scratch0, enc.X14, 0)
			e.fail(enc.NE, enc.Scratch0, enc.X29)
			a.Constant64(enc.X29, int64(f.job.LinMemBase()))
			a.Ld(enc.Scratch0, enc.X19, 16)
			e.fail(enc.NE, enc.Scratch0, enc.X29)
			a.Constant64(enc.X29, int64(t.codeBase()+uintptr(t.info.Entry)))
			e.fail(enc.NE, enc.X16, enc.X29)
			a.Constant64(enc.X29, int64(t.job.LinMemBase()))
			e.fail(enc.NE, enc.X17, enc.X29)
			a.Constant64(enc.X29, int64(tailRAPointer(t.context)))
			e.fail(enc.NE, enc.X18, enc.X29)
			a.Ld(enc.Scratch0, enc.X15, 24)
			a.Constant64(enc.X29, int64(o.targetFlags(i)))
			e.fail(enc.NE, enc.Scratch0, enc.X29)
			// The actual producer tuple has now matched frozen physical owners.
			// Authenticate its anchor using constants before reading anchor data.
			a.Ld(enc.Scratch0, enc.X18, 16)
			a.Constant64(enc.X29, int64(tailRAPointer(t.directory)))
			e.fail(enc.NE, enc.Scratch0, enc.X29)
			a.Ld(enc.Scratch0, enc.X29, 32)
			e.fail(enc.NE, enc.Scratch0, enc.X18)
			a.Ld(enc.Scratch0, enc.X29, 16)
			e.fail(enc.NE, enc.Scratch0, enc.X17)
			genuine = append(genuine, a.JumpLong(false))
			e.patch(next)
		}
	}
	e.fail(enc.EQ, enc.Zero, enc.Zero)
	for _, site := range genuine {
		e.patch(site)
	}
}
