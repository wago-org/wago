//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

// The complete entry directory remains physical, including ordinal zero.
// Only the separate optional signatures describe callable numeric targets.
func sparseOwnerFixture(t *testing.T, holes []bool) (*NativeTailDefinedOwner, *tailPublishedOwnerForTest, []int, []*wasm.CompType) {
	t.Helper()
	functions := make([]tailRAFunction, len(holes))
	signatures := make([]*wasm.CompType, len(holes))
	entries := make([]int, len(holes))
	var code []byte
	for i := range holes {
		sig := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.F64}, Results: []wasm.ValType{wasm.V128}}
		functions[i] = tailRAProofFunctionFixture(t, sig, abi.RV64InstanceContextBytes)
		for len(code)&15 != 0 {
			code = append(code, 0x13, 0, 0, 0)
		}
		entries[i] = len(code)
		code = append(code, functions[i].snapshot...)
		signatures[i] = sig
	}
	buffer := tailRAProofBuffer(t, code)
	for i := range functions {
		f := &functions[i]
		f.code, f.snapshot, f.info.Entry = buffer, code, entries[i]
		end := len(code)
		if i+1 < len(functions) {
			end = entries[i+1]
		}
		f.info.CodeBytes, f.info.mode.functionIndex = end-entries[i], uint32(i)
		f.job, f.context = functions[0].job, functions[0].context
	}
	o := tailDynamicOwnerForTest(t, functions)
	for i, hole := range holes {
		if hole {
			clear(o.descriptors[40*(i+1) : 40*(i+2)])
			clear(o.directory[32+32*i : 32+32*(i+1)])
			signatures[i] = nil
		}
	}
	f, lease := tailPublishedFunctionForTest(t, o.functions[0])
	owner, err := NewNativeTailSparseDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, entries, signatures)
	if err != nil {
		t.Fatal(err)
	}
	return owner, lease, entries, signatures
}

func TestNativeTailSparseDefinedOwnerPhysicalSpans(t *testing.T) {
	for _, holes := range [][]bool{{true, false, false}, {false, true, false}, {false, false, true}, {true, false, true}, {true, true, true}, {false, false, false}} {
		t.Run(fmt.Sprint(holes), func(t *testing.T) {
			owner, lease, entries, signatures := sparseOwnerFixture(t, holes)
			nonCallable := 0
			for i, f := range owner.owner.functions {
				end := len(lease.view)
				if i+1 < len(entries) {
					end = entries[i+1]
				}
				if f.info.Entry != entries[i] || f.info.CodeBytes != end-entries[i] || f.info.mode.functionIndex != uint32(i) || f.job != owner.owner.functions[0].job || owner.owner.uncallable[i] != holes[i] || (f.signature == nil) != holes[i] {
					t.Fatal("lost physical ordinal/span", i)
				}
				if holes[i] {
					nonCallable++
					if owner.owner.signatureKeys[i] != 0 || f.info.Params != 0 || f.info.Results != 0 {
						t.Fatal("invented callable signature", i)
					}
				} else {
					signatures[i].Params[0] = wasm.F32
					if !wasm.EqualValType(f.signature.Params[0], wasm.I32) {
						t.Fatal("signature alias")
					}
				}
				signatures[i] = &wasm.CompType{Kind: wasm.CompFunc}
				entries[i] += 16
			}
			if err := owner.Validate(); err != nil {
				t.Fatal("input mutation changed frozen physical/callable rows", err)
			}
			if n := testing.AllocsPerRun(4, func() {
				if err := owner.Validate(); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatal("warm sparse owner allocation", n)
			}
			counts := NativeTailOwnerCounts{Owners: 1, Functions: 3, Values: (3 - nonCallable) * 3, MaskSlots: 3, NonCallable: nonCallable}
			for _, sites := range []int{1, 4} {
				metadata, code, err := NativeTailFactoryCountsBudget(sites, 3*sites, 1, counts, len(lease.view))
				actualMetadata, actualCode, actualErr := NativeTailDefinedFactoryBudget(sites, 3*sites, 1, []*NativeTailDefinedOwner{owner}, len(lease.view))
				if err != nil || actualErr != nil || metadata != actualMetadata || code != actualCode {
					t.Fatal("count-only/frozen sparse charge", metadata, actualMetadata, code, actualCode, err, actualErr)
				}
			}
			d := tailRADynamicSite{owners: []tailRADynamicOwner{owner.owner}}
			frozen := d.freeze()
			owner.owner.uncallable[0] = !owner.owner.uncallable[0]
			if frozen.owners[0].uncallable[0] != holes[0] || frozen.owners[0].validate(0) != nil {
				t.Fatal("factory mask aliases owner")
			}
			owner.owner.uncallable[0] = holes[0]
			// An expired physical image must win before any borrowed row is read.
			lease.err = errors.New("sparse physical image retired")
			owner.owner.context[16] ^= 8
			if err := owner.Validate(); !errors.Is(err, lease.err) {
				t.Fatal("retired sparse physical owner reused", err)
			}
			owner.owner.context[16] ^= 8
		})
	}
}

func TestNativeTailSparseDefinedOwnerRejectsCallableHoles(t *testing.T) {
	owner, _, entries, signatures := sparseOwnerFixture(t, []bool{true, false, true})
	for _, i := range []int{0, 2} {
		for _, row := range [][]byte{owner.owner.descriptors[40*(i+1) : 40*(i+2)], owner.owner.directory[32+32*i : 32+32*(i+1)]} {
			for off := range row {
				row[off] = 1
				if err := owner.Validate(); !errors.Is(err, ErrUnsupported) {
					t.Fatal("nonzero internal-only row", i, off, err)
				}
				f := owner.owner.functions[0]
				if rejected, err := NewNativeTailSparseDefinedOwner(f.published, f.job, owner.owner.context, owner.owner.descriptors, owner.owner.directory, entries, signatures); rejected != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("constructor accepted internal-only row", i, off, err)
				}
				row[off] = 0
			}
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*tailRADynamicOwner)
	}{
		{"mask-short", func(o *tailRADynamicOwner) { o.uncallable = o.uncallable[:2] }},
		{"mask-missing", func(o *tailRADynamicOwner) { o.uncallable = nil }},
		{"mask-flipped", func(o *tailRADynamicOwner) { o.uncallable[0] = false }},
		{"key", func(o *tailRADynamicOwner) { o.signatureKeys[0] = 1 }},
		{"signature", func(o *tailRADynamicOwner) { o.functions[0].signature = &wasm.CompType{} }},
		{"params", func(o *tailRADynamicOwner) { o.functions[0].info.Params = 1 }},
		{"results", func(o *tailRADynamicOwner) { o.functions[0].info.Results = 1 }},
		{"targets", func(o *tailRADynamicOwner) { o.targets = append([]tailRAFunction(nil), o.functions...) }},
		{"proxies", func(o *tailRADynamicOwner) { o.proxies = make([]*tailRADynamicProxy, 3) }},
		{"hosts", func(o *tailRADynamicOwner) { o.hosts = make([]*NativeTailHostFunction, 3) }},
		{"job", func(o *tailRADynamicOwner) { o.functions[0].job = nil }},
		{"entry", func(o *tailRADynamicOwner) { o.functions[0].info.Entry += 16 }},
		{"span", func(o *tailRADynamicOwner) { o.functions[0].info.CodeBytes = len(o.functions[0].snapshot) + 4 }},
		{"frame", func(o *tailRADynamicOwner) { o.functions[0].info.FrameBytes += 16 }},
		{"header", func(o *tailRADynamicOwner) { o.functions[0].info.FrameHeaderBytes = 16 }},
		{"invocation", func(o *tailRADynamicOwner) { o.functions[0].info.mode.invocationContext = false }},
		{"context", func(o *tailRADynamicOwner) { o.functions[0].context = bytes.Clone(o.context) }},
		{"anchor", func(o *tailRADynamicOwner) { o.functions[0].directory = bytes.Clone(o.descriptors) }},
		{"snapshot", func(o *tailRADynamicOwner) {
			o.functions[0].snapshot = bytes.Clone(o.functions[0].snapshot)
			o.functions[0].snapshot[0] ^= 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := (&tailRADynamicSite{owners: []tailRADynamicOwner{owner.owner}}).freeze()
			tc.edit(&d.owners[0])
			bad := NativeTailDefinedOwner{owner: d.owners[0]}
			if err := bad.Validate(); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid sparse ownership accepted", err)
			}
		})
	}
	f := owner.owner.functions[0]
	signatures[0] = &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}
	if rejected, err := NewNativeTailSparseDefinedOwner(f.published, f.job, owner.owner.context, owner.owner.descriptors, owner.owner.directory, entries, signatures); rejected != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("sparse constructor generalized callable reference ABI", err)
	}
}

func TestNativeTailSparseDefinedOwnerInactiveMetadata(t *testing.T) {
	owner, _, entries, signatures := sparseOwnerFixture(t, []bool{false, false, false})
	f := owner.owner.functions[0]
	for _, sparse := range []bool{false, true} {
		construct := func() (*NativeTailDefinedOwner, error) {
			if sparse {
				return NewNativeTailSparseDefinedOwner(f.published, f.job, owner.owner.context, owner.owner.descriptors, owner.owner.directory, entries, signatures)
			}
			return NewNativeTailDefinedOwner(f.published, f.job, owner.owner.context, owner.owner.descriptors, owner.owner.directory, entries, []wasm.CompType{*signatures[0], *signatures[1], *signatures[2]})
		}
		if valid, err := construct(); valid == nil || err != nil {
			t.Fatal("callable descriptor/key oracle", sparse, err)
		}
		original := *signatures[1]
		for _, change := range []func(*wasm.CompType){
			func(s *wasm.CompType) { s.Fields = []wasm.FieldType{{}} },
			func(s *wasm.CompType) { s.Array = wasm.NewFieldType(wasm.StorageVal(wasm.I64), wasm.Var) },
		} {
			change(signatures[1])
			if rejected, err := construct(); rejected != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("constructor retained uncharged inactive metadata", sparse, err)
			}
			*signatures[1] = original
		}
		// Empty syntax must not retain the large inactive backing array.
		signatures[1].Fields = make([]wasm.FieldType, 8192)[:0]
		valid, err := construct()
		if valid == nil || err != nil || valid.owner.functions[1].signature.Fields != nil {
			t.Fatal("constructor retained empty inactive backing", sparse, err)
		}
		*signatures[1] = original
	}
}

func TestNativeTailSparseDefinedOwnerCountBounds(t *testing.T) {
	for _, tuple := range [][3]int{{0, 0, 0}, {MaxFunctions + 1, 0, 0}, {1, -1, 0}, {1, 2, 0}, {1, 1, 1}, {2, 1, 513}, {2, 0, -1}} {
		if _, err := NativeTailSparseDefinedOwnerBytes(tuple[0], tuple[1], tuple[2]); !errors.Is(err, ErrUnsupported) {
			t.Fatal("sparse initial bound", tuple, err)
		}
	}
	if _, err := NativeTailSparseDefinedOwnerBytes(MaxFunctions, MaxFunctions, 0); err != nil {
		t.Fatal("bounded all-hole physical owner", err)
	}
	for _, change := range []func(*NativeTailOwnerCounts){
		func(c *NativeTailOwnerCounts) { c.MaskSlots = -1 }, func(c *NativeTailOwnerCounts) { c.MaskSlots = 4 }, func(c *NativeTailOwnerCounts) { c.NonCallable = -1 }, func(c *NativeTailOwnerCounts) { c.NonCallable = 4 }, func(c *NativeTailOwnerCounts) { c.MaskSlots = 1 }, func(c *NativeTailOwnerCounts) { c.Values = 513 },
	} {
		c := NativeTailOwnerCounts{Owners: 1, Functions: 3, Values: 3, MaskSlots: 3, NonCallable: 2}
		change(&c)
		if _, _, err := NativeTailFactoryCountsBudget(1, 3, 1, c, 4096); !errors.Is(err, ErrUnsupported) {
			t.Fatal("sparse frozen bound", c, err)
		}
	}
}

func TestNativeTailSparseClosedReferenceModule(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			m := closedReferenceDecode(t, vector, tails, true, false)
			c, err := closedReferenceCompile(m, vector, tails)
			if err != nil {
				t.Fatal(err)
			}
			physical := tailRAProofFunctionFixture(t, &wasm.CompType{Kind: wasm.CompFunc}, abi.RV64InstanceContextBytes)
			buffer := tailRAProofBuffer(t, c.code)
			functions := make([]tailRAFunction, len(c.functions))
			entries := make([]int, len(functions))
			signatures := make([]*wasm.CompType, len(functions))
			for i, info := range c.functions {
				sig, _ := m.FuncSignature(uint32(i))
				functions[i] = tailRAFunction{code: buffer, snapshot: c.code, info: info, signature: sig, job: physical.job, context: physical.context}
				entries[i], signatures[i] = info.Entry, sig
			}
			o := tailDynamicOwnerForTest(t, functions)
			for _, i := range []int{2, 3} {
				clear(o.descriptors[40*(i+1) : 40*(i+2)])
				clear(o.directory[32+32*i : 32+32*(i+1)])
				signatures[i] = nil
			}
			f, _ := tailPublishedFunctionForTest(t, o.functions[0])
			owner, err := NewNativeTailSparseDefinedOwner(f.published, f.job, o.context, o.descriptors, o.directory, entries, signatures)
			if err != nil {
				t.Fatal("real internal reference-valued physical image", vector, tails, err)
			}
			for _, i := range []int{2, 3} {
				if !owner.owner.uncallable[i] || owner.owner.functions[i].signature != nil || owner.owner.functions[i].info.FrameBytes != c.functions[i].FrameBytes {
					t.Fatal("actual reference function lost its physical frame", i)
				}
			}
		}
	}
}
