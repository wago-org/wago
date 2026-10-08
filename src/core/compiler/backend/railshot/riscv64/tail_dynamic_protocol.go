//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// Private native-only dynamic proof. All mappings are exclusively retained by
// the invocation owner through execution and parks. Imported proxies and host
// rows remain closed; this stage selects genuine defined framed functions.
type tailRADynamicOwner struct {
	descriptors, context, directory []byte
	functions                       []tailRAFunction
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
		if len(o.functions) == 0 || len(o.functions) > MaxFunctions-count || len(o.descriptors) != 40*(len(o.functions)+1) || len(o.context) != abi.RV64InstanceContextBytes || len(o.directory) != abi.RV64TailTargetsHeaderBytes+abi.RV64TailTargetsRowBytes*len(o.functions) {
			return 0, 0, unsupported("dynamic tail retained owner extent")
		}
		count += len(o.functions)
		metadata += len(o.functions) * int(unsafe.Sizeof(tailRAFunction{}))
		for _, f := range o.functions {
			if f.signature == nil || len(f.signature.Params) > 256 || len(f.signature.Results) > 256 {
				return 0, 0, unsupported("dynamic tail frozen signature extent")
			}
			metadata += int(unsafe.Sizeof(wasm.CompType{})) + (len(f.signature.Params)+len(f.signature.Results))*int(unsafe.Sizeof(wasm.ValType{}))
		}
		code += 512 * len(o.functions)
		if metadata > MaxModuleBytes || code > MaxCodeBytes {
			return 0, 0, unsupported("dynamic tail metadata/code budget")
		}
	}
	return metadata, code, nil
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
		anchor, ctx, dir := tailRAPointer(o.descriptors), tailRAPointer(o.context), tailRAPointer(o.directory)
		if !(tailRARange{anchor, uintptr(len(o.descriptors))}).valid(8) || !(tailRARange{dir, uintptr(len(o.directory))}).valid(8) || ctx == 0 || ctx&7 != 0 || binary.LittleEndian.Uint64(o.context[16:]) != uint64(anchor) || binary.LittleEndian.Uint64(o.descriptors[32:]) != uint64(ctx) || binary.LittleEndian.Uint64(o.descriptors[8:]) != uint64(dir) || binary.LittleEndian.Uint64(o.directory) != abi.RV64TailTargetsMagic || binary.LittleEndian.Uint32(o.directory[8:]) != abi.RV64TailTargetsVersion || binary.LittleEndian.Uint32(o.directory[12:]) != uint32(len(o.functions)) || binary.LittleEndian.Uint64(o.directory[16:]) != uint64(anchor) || binary.LittleEndian.Uint64(o.directory[24:]) != uint64(ctx) {
			return unsupported("dynamic tail retained directory identity")
		}
		for _, prev := range d.owners[:n] {
			p := tailRAPointer(prev.descriptors)
			if anchor < p+uintptr(len(prev.descriptors)) && p < anchor+uintptr(len(o.descriptors)) || tailRAPointer(prev.context) == ctx {
				return unsupported("dynamic tail ambiguous retained owner")
			}
		}
		for i, f := range o.functions {
			if err := f.validate(true); err != nil {
				return err
			}
			entry, home := f.code.Base()+uintptr(f.info.Entry), f.job.LinMemBase()
			key := wasm.StructuralFuncTypeKey(f.signature)
			desc := o.descriptors[40*(i+1):]
			row := o.directory[abi.RV64TailTargetsHeaderBytes+i*abi.RV64TailTargetsRowBytes:]
			if !f.info.mode.invocationContext || tailRAPointer(f.context) != ctx || tailRAPointer(f.directory) != anchor || binary.LittleEndian.Uint64(o.descriptors[16:]) != uint64(home) || binary.LittleEndian.Uint64(desc) != uint64(entry) || binary.LittleEndian.Uint64(desc[8:]) != key || binary.LittleEndian.Uint64(desc[16:]) != uint64(home)|abi.FuncRefLocalWrapperHomeTag || binary.LittleEndian.Uint64(desc[24:]) != uint64(anchor+uintptr(40*(i+1))) || binary.LittleEndian.Uint64(desc[32:]) != uint64(ctx) || binary.LittleEndian.Uint64(row) != uint64(entry) || binary.LittleEndian.Uint64(row[8:]) != uint64(home) || binary.LittleEndian.Uint64(row[16:]) != uint64(ctx) || binary.LittleEndian.Uint64(row[24:]) != 0 {
				return unsupported("dynamic tail genuine defined target")
			}
		}
	}
	return nil
}

func (d *tailRADynamicSite) freeze() *tailRADynamicSite {
	if d == nil {
		return nil
	}
	f := *d
	f.owners = append([]tailRADynamicOwner(nil), d.owners...)
	for i := range f.owners {
		f.owners[i].functions = append([]tailRAFunction(nil), d.owners[i].functions...)
		for n := range f.owners[i].functions {
			fn := &f.owners[i].functions[n]
			sig := *fn.signature
			sig.Params = append([]wasm.ValType(nil), sig.Params...)
			sig.Results = append([]wasm.ValType(nil), sig.Results...)
			fn.signature = &sig
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
			for fn, f := range o.functions {
				seen := false
			previous:
				for pn, p := range sites[:sn+1] {
					if p.dynamic == nil {
						continue
					}
					for po, owner := range p.dynamic.owners {
						for pf, prev := range owner.functions {
							if pn == sn && po == on && pf == fn {
								break previous
							}
							if prev.code == f.code {
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
	a.Ld(enc.X29, enc.X15, 24)
	e.fail(enc.NE, enc.X29, enc.Zero) // Host/proxy rows remain closed.
	e.fail(enc.NE, enc.X18, enc.X20)
	a.Ld(enc.X29, enc.X19, 16)
	e.fail(enc.NE, enc.X29, enc.X17)
	a.Constant64(enc.X29, int64(abi.FuncRefLocalWrapperHomeTag))
	a.Or(enc.X29, enc.X17, enc.X29)
	a.Ld(enc.Scratch0, enc.X14, 16)
	e.fail(enc.NE, enc.X29, enc.Scratch0)
	a.Ld(enc.X29, enc.X14, 0)
	e.fail(enc.NE, enc.X29, enc.X16)
	// Authenticate the exact frozen entry as well as the live directory. Even
	// coordinated descriptor/row corruption cannot substitute arbitrary code.
	var genuine []int
	for _, o := range d.owners {
		for i, f := range o.functions {
			// A live key cannot relabel a genuine function of another signature.
			if wasm.StructuralFuncTypeKey(f.signature) != d.key {
				continue
			}
			a.Constant64(enc.X29, int64(tailRAPointer(o.descriptors)+uintptr(40*(i+1))))
			next := a.BranchLong(enc.NE, enc.X14, enc.X29)
			a.Constant64(enc.X29, int64(f.code.Base()+uintptr(f.info.Entry)))
			e.fail(enc.NE, enc.X16, enc.X29)
			a.Constant64(enc.X29, int64(f.job.LinMemBase()))
			e.fail(enc.NE, enc.X17, enc.X29)
			genuine = append(genuine, a.JumpLong(false))
			e.patch(next)
		}
	}
	e.fail(enc.EQ, enc.Zero, enc.Zero)
	for _, site := range genuine {
		e.patch(site)
	}
}
