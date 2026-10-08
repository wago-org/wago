//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

// Symbolic metadata proof only: these imported-owner dynamic fixtures remain
// rejected before mapping. Genuine definition-only indirect source/artifact
// execution is covered separately in public_dynamic_tail_riscv64_test.go.
func rv64TailSymbolicFixture(t *testing.T, kind uint32, vector bool) *Compiled {
	t.Helper()
	values := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
	if vector {
		values = append(values, wasm.V128)
	}
	base := rv64NativeTailCompile(t, "c2p-symbolic", rv64PublicTailModule(values, false), false)
	c := cloneCompiledMetadata(base)
	c.validateMemo, c.codeCache = &validateMemo{}, nil
	d := DefinedTypeDescriptor{Kind: CompositeTypeFunction, Final: true}
	for _, v := range c.importFuncSigs[0].Params {
		switch v {
		case ValI32:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeI32})
		case ValI64:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeI64})
		case ValF32:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeF32})
		case ValF64:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeF64})
		case ValV128:
			d.Params = append(d.Params, ValueTypeDescriptor{Kind: ValueTypeV128})
		default:
			t.Fatal("unexpected fixture signature")
		}
	}
	d.Results = slices.Clone(d.Params)
	c.Types = append(slices.Clone(c.Types), DefinedTypeDescriptor{Kind: CompositeTypeFunction, Final: true}, d)
	p := rv64NativeTailModule(base).Plans[0]
	p.Kind, p.TypeIndex = kind, uint32(len(c.Types)-1)
	p.ImportIndex = 0
	if vector {
		p.SourceStride = 16
	}
	p.ReferenceBase = p.SourceBase + len(d.Params)*p.SourceStride
	p.FrameBytes = (p.ReferenceBase + 8 + 15) &^ 15
	c.NeedsFuncRefDescs = true
	if kind == encoder.NativeTailIndirect {
		c.HasTable, c.TableType, c.TableSize = true, ValFuncRef, 1
	}
	rv64StoreNativeTailModule(c, &encoder.NativeTailModule{Plans: []encoder.NativeTailPlan{p}})
	if err := rv64ValidateNativeTail(c); err != nil {
		t.Fatal("valid symbolic fixture", err)
	}
	return c
}

func rv64TailCodecView(c *Compiled) *Compiled {
	v := *c
	v.validateMemo, v.codeCache = &validateMemo{}, nil
	return &v
}

func TestRV64PrivateTailSymbolicVersion2AndClosedPublication(t *testing.T) {
	for _, kind := range []uint32{encoder.NativeTailIndirect, encoder.NativeTailReference} {
		for _, vector := range []bool{false, true} {
			c := rv64TailSymbolicFixture(t, kind, vector)
			w := compiledWriter{}
			if err := railshotWriteNativeTail(&w, c); err != nil {
				t.Fatal(err)
			}
			if len(w.buf) != 41 || binary.LittleEndian.Uint32(w.buf) != 2 {
				t.Fatal("dynamic wire version/size")
			}
			v := rv64TailCodecView(c)
			r := compiledReader{data: w.buf, budget: newArtifactDecodeBudget(0)}
			if err := railshotReadNativeTail(&r, v); err != nil || len(r.data) != 0 || !slices.Equal(rv64NativeTailModule(c).Plans, rv64NativeTailModule(v).Plans) {
				t.Fatal("exact symbolic roundtrip", err)
			}
			w2 := compiledWriter{}
			if err := railshotWriteNativeTail(&w2, v); err != nil || !bytes.Equal(w.buf, w2.buf) {
				t.Fatal("symbolic wire identity", err)
			}
			if a := testing.AllocsPerRun(4, func() {
				if err := rv64ValidateNativeTail(v); err != nil {
					panic(err)
				}
			}); a != 0 {
				t.Fatal("metadata validation copied declared signatures", a)
			}
			if err := railshotValidateCompiled(v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("unsupported dynamic shape admission opened", err)
			}
			cache := v.loadCodeCache()
			base, sealed, refs := cache.base, cache.sealed, cache.refs
			if in, err := Instantiate(v, testImports("env.tail", HostCallFunc(func(HostCall) {}))); in != nil || !errors.Is(err, railshot.ErrUnsupported) || cache.base != base || cache.sealed != sealed || cache.refs != refs {
				t.Fatal("symbolic dynamic plan reached mapping", err)
			}
		}
	}
}

func TestRV64PrivateTailSymbolicInvalidFieldsAndAllocationBudget(t *testing.T) {
	c := rv64TailSymbolicFixture(t, encoder.NativeTailReference, true)
	w := compiledWriter{}
	if err := railshotWriteNativeTail(&w, c); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func([]byte, *Compiled)
	}{
		{"version", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b, 3) }},
		{"kind", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[5:], 3) }},
		{"type-at-count", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[9:], uint32(len(v.Types))) }},
		{"wrong-type-namespace", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[9:], uint32(len(v.Types)-2)) }},
		{"unused-import", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[21:], 1) }},
		{"reference-header", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[13:], 40) }},
		{"reference-end", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[13:], binary.LittleEndian.Uint32(b[29:])) }},
		{"reference-unaligned", func(b []byte, v *Compiled) {
			binary.LittleEndian.PutUint32(b[13:], binary.LittleEndian.Uint32(b[13:])+1)
		}},
		{"reference-swar-home", func(b []byte, v *Compiled) {
			binary.LittleEndian.PutUint32(b[13:], binary.LittleEndian.Uint32(b[13:])-8)
		}},
		{"source-unaligned", func(b []byte, v *Compiled) {
			binary.LittleEndian.PutUint32(b[33:], binary.LittleEndian.Uint32(b[33:])+1)
		}},
		{"source-frame", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[29:], 0xfffffff0) }},
		{"vector-stride", func(b []byte, v *Compiled) { binary.LittleEndian.PutUint32(b[37:], 8) }},
		{"declared-reference", func(b []byte, v *Compiled) {
			v.Types = slices.Clone(v.Types)
			d := &v.Types[len(v.Types)-1]
			d.Params = slices.Clone(d.Params)
			d.Params[0] = ValueTypeDescriptor{Kind: ValueTypeReference}
		}},
		{"declared-nonfinal", func(b []byte, v *Compiled) { v.Types = slices.Clone(v.Types); v.Types[len(v.Types)-1].Final = false }},
		{"declared-super", func(b []byte, v *Compiled) {
			v.Types = slices.Clone(v.Types)
			v.Types[len(v.Types)-1].Supers = []uint32{0}
		}},
		{"declared-physical-cells", func(b []byte, v *Compiled) {
			v.Types = slices.Clone(v.Types)
			d := &v.Types[len(v.Types)-1]
			d.Params = make([]ValueTypeDescriptor, 129)
			for i := range d.Params {
				d.Params[i].Kind = ValueTypeV128
			}
		}},
		{"missing-descriptors", func(b []byte, v *Compiled) {
			v.NeedsFuncRefDescs = false
			v.HasTable = false
			v.needsFuncRefContextHeader = false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, v := bytes.Clone(w.buf), rv64TailCodecView(c)
			tc.mutate(b, v)
			r := compiledReader{data: b, budget: newArtifactDecodeBudget(0)}
			if err := railshotReadNativeTail(&r, v); err == nil {
				t.Fatal("invalid symbolic plan accepted")
			}
		})
	}
	for n := 0; n < len(w.buf); n++ {
		r := compiledReader{data: w.buf[:n], budget: newArtifactDecodeBudget(0)}
		if err := railshotReadNativeTail(&r, rv64TailCodecView(c)); err == nil {
			t.Fatal("truncated dynamic plan", n)
		}
	}
	actual := int64(artifactCollectionCopies * unsafe.Sizeof(encoder.NativeTailPlan{}))
	for _, limit := range []int64{actual - 1, actual} {
		v := rv64TailCodecView(c)
		r := compiledReader{data: w.buf, budget: newArtifactDecodeBudget(limit)}
		err := railshotReadNativeTail(&r, v)
		if limit < actual {
			if err == nil || rv64NativeTailModule(v) != nil {
				t.Fatal("undercharged Go footprint", err)
			}
		} else if err != nil {
			t.Fatal("exact plan footprint", err)
		}
	}
}

func TestRV64PrivateTailSymbolicDirectVersion1AndVersion2Compatibility(t *testing.T) {
	c := rv64NativeTailCompile(t, "c2p-direct-v1", rv64PublicTailModule([]wasm.ValType{wasm.I32}, false), false)
	w := compiledWriter{}
	if err := railshotWriteNativeTail(&w, c); err != nil {
		t.Fatal(err)
	}
	if len(w.buf) != 29 || binary.LittleEndian.Uint32(w.buf) != 1 {
		t.Fatal("direct v1 wire changed")
	}
	// Explicit v2 direct rows are accepted only with zero unused type/ref fields.
	v2 := append([]byte{}, w.buf[:5]...)
	binary.LittleEndian.PutUint32(v2, 2)
	v2 = append(v2, make([]byte, 12)...)
	v2 = append(v2, w.buf[5:]...)
	v := rv64TailCodecView(c)
	r := compiledReader{data: v2, budget: newArtifactDecodeBudget(0)}
	if err := railshotReadNativeTail(&r, v); err != nil || !slices.Equal(rv64NativeTailModule(v).Plans, rv64NativeTailModule(c).Plans) {
		t.Fatal("v2 direct compatibility", err)
	}
	w2 := compiledWriter{}
	if err := railshotWriteNativeTail(&w2, v); err != nil || !bytes.Equal(w2.buf, w.buf) {
		t.Fatal("canonical direct v1 rewrite", err)
	}
	for _, off := range []int{9, 13} {
		b := bytes.Clone(v2)
		binary.LittleEndian.PutUint32(b[off:], 1)
		r := compiledReader{data: b, budget: newArtifactDecodeBudget(0)}
		if err := railshotReadNativeTail(&r, rv64TailCodecView(c)); err == nil {
			t.Fatal("noncanonical unused direct field", off)
		}
	}
	actual := int64(artifactCollectionCopies * unsafe.Sizeof(encoder.NativeTailPlan{}))
	r = compiledReader{data: w.buf, budget: newArtifactDecodeBudget(actual - 1)}
	if err := railshotReadNativeTail(&r, rv64TailCodecView(c)); err == nil {
		t.Fatal("v1 decoder used stale Go plan footprint")
	}
}

func TestRV64PrivateTailSymbolicMixedRowsAndScalarSWAROverlap(t *testing.T) {
	c := rv64TailSymbolicFixture(t, encoder.NativeTailReference, false)
	p := rv64NativeTailModule(c).Plans[0]
	p.SourceStride = 16
	p.ReferenceBase = p.SourceBase + len(c.importFuncSigs[0].Params)*16
	p.FrameBytes = (p.ReferenceBase + 8 + 15) &^ 15
	direct := p
	direct.Kind, direct.TypeIndex, direct.ReferenceBase = encoder.NativeTailDirectImport, 0, 0
	rv64StoreNativeTailModule(c, &encoder.NativeTailModule{Plans: []encoder.NativeTailPlan{direct, p}})
	w := compiledWriter{}
	if err := railshotWriteNativeTail(&w, c); err != nil || len(w.buf) != 77 || binary.LittleEndian.Uint32(w.buf) != 2 {
		t.Fatal("mixed v2 rows", err)
	}
	v := rv64TailCodecView(c)
	r := compiledReader{data: w.buf, budget: newArtifactDecodeBudget(0)}
	if err := railshotReadNativeTail(&r, v); err != nil || !slices.Equal(rv64NativeTailModule(v).Plans, rv64NativeTailModule(c).Plans) {
		t.Fatal("mixed direct/dynamic identity", err)
	}
	// Every parameter is scalar. The last zero high-half belongs to the
	// complete stride16 parameter home and cannot become a reference spill.
	bad := p
	bad.ReferenceBase -= 8
	rv64StoreNativeTailModule(v, &encoder.NativeTailModule{Plans: []encoder.NativeTailPlan{bad}})
	if err := rv64ValidateNativeTail(v); err == nil {
		t.Fatal("scalar SWAR zero high-half accepted as reference")
	}
	// Direct rows in a mixed v2 appendix retain canonical unused fields too.
	for _, off := range []int{9, 13} {
		blob := bytes.Clone(w.buf)
		binary.LittleEndian.PutUint32(blob[off:], 1)
		r := compiledReader{data: blob, budget: newArtifactDecodeBudget(0)}
		if err := railshotReadNativeTail(&r, rv64TailCodecView(c)); err == nil {
			t.Fatal("mixed direct unused field accepted", off)
		}
	}
}
