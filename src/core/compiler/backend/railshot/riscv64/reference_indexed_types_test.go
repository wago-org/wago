//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceIndexedTypesModule(t *testing.T, vector bool, form byte, shape string, target byte) *wasm.Module {
	t.Helper()
	types := make([][]byte, 65)
	for i := range types {
		types[i] = wasmtest.FuncType(nil, nil)
	}
	types[2] = wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	if vector {
		types[3] = wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil)
	}
	// Types 0, 1 and 64 are structurally equivalent but distinct indices.
	refType := []byte{form, 0xc0, 0}
	var body []byte
	switch shape {
	case "local":
		body = []byte{0xd2, target, 0x22, 0, 0x1a, 0x20, 0, 0x1a, 0x0b}
	case "block":
		body = append([]byte{2}, refType...)
		body = append(body, 0xd2, target, 0x0c, 0, 0x0b, 0x1a, 0x0b)
	case "select":
		body = append([]byte{0xd2, 0, 0xd2, target, 0x41, 1, 0x1c, 1}, refType...)
		body = append(body, 0x1a, 0x0b)
	}
	m, err := wasm.DecodeModule(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{4, 0, 1, 2, 0}), wasmtest.Section(9, []byte{1, 3, 0, 3, 0, 1, 2}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0x0b}), wasmtest.Code(body)))))
	if err != nil {
		t.Fatal(err)
	}
	if shape == "local" {
		m.Code[3].Locals.Runs = []wasm.LocalRun{{Count: 1, Type: wasm.RefVal(wasm.Ref(form == 0x63, wasm.IndexedHeap(wasm.TypeIdx{Index: 64}), false))}}
	}
	return m
}

func TestReferenceIndexedTypesPrivateAndProductionContract(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, form := range []byte{0x63, 0x64} {
			for _, shape := range []string{"local", "block", "select"} {
				for _, dead := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-form%x-%s-dead%t", vector, form, shape, dead), func(t *testing.T) {
						m := referenceIndexedTypesModule(t, vector, form, shape, 1)
						if dead {
							m.Code[3].BodyBytes = append([]byte{0}, m.Code[3].BodyBytes...)
						}
						if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
							t.Fatal("equivalent indexed type fixture", err)
						}
						types := make([]*wasm.CompType, len(m.Types))
						specs := make([]functionSpec, len(types))
						for i := range types {
							types[i] = &m.Types[i].SubTypes[0].Comp
							specs[i].signature = types[i]
						}
						functions := []*wasm.CompType{types[0], types[1], types[2], types[0]}
						for _, permission := range []bool{false, true} {
							state := moduleState{production: true, importReferences: true, referenceCalls: permission, types: specs, functions: []functionSpec{specs[0], specs[1], specs[2], specs[0]}}
							_, a := walkInteger(&m.Code[3], types[0], &state, nil)
							_, b := walkSWARPairsWithTypedCallMode(&m.Code[3], types[0], memoryProfile{}, types, functions, nil, 0, nil, nil, 1, 0, true, false, false, false, false, false, permission, nil)
							if (a == nil) != permission || (b == nil) != permission {
								t.Fatal("private indexed annotation permission", permission, a, b)
							}
						}
						// The annotation itself supplies a witness, including nullable
						// and dead forms, independently of other typed instructions.
						for _, otherWitness := range []bool{false, true} {
							if otherWitness {
								m.Code[3].BodyBytes = append([]byte{0xd0, 0, 0x1a}, m.Code[3].BodyBytes...)
							}
							for _, permission := range []bool{false, true} {
								pressure := 0
								image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: permission, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
								if !permission {
									if image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
										t.Fatal("indexed annotation permission bypassed", image, err, pressure)
									}
								} else if err != nil || image == nil || !image.NativeReferenceControl || image.NativeReferenceCalls || image.NativeTail != nil || image.CodeImage != nil || pressure != 1 {
									t.Fatal("indexed annotation witness/staging", image, err, pressure)
								}
							}
						}
					})
				}
			}
		}
	}
}

func TestReferenceIndexedTypesDirectory(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			sig  *wasm.CompType
			ok   bool
		}{
			{"function", ft, true},
			{"nil", nil, false},
			{"struct", &wasm.CompType{Kind: wasm.CompStruct}, false},
			{"reference-param", &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}, false},
			{"reference-result", &wasm.CompType{Kind: wasm.CompFunc, Results: []wasm.ValType{wasm.FuncRef}}, false},
			{"vector", &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}, vector},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, tc.name), func(t *testing.T) {
				dir := internalReferenceTypes{scalar: []functionSpec{{signature: tc.sig}}}
				if vector {
					dir = internalReferenceTypes{swar: []*wasm.CompType{tc.sig}}
				}
				for _, nullable := range []bool{false, true} {
					typ := wasm.RefVal(wasm.Ref(nullable, wasm.IndexedHeap(wasm.TypeIdx{}), false))
					if got := internalFunctionReferenceWithTypes(typ, true, dir); got != tc.ok || internalFunctionReferenceWithTypes(typ, false, dir) {
						t.Fatal("resolved indexed reference", got, tc.ok)
					}
				}
				for _, typ := range []wasm.ValType{
					wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1}), false)),
					wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Rec: true}), false)),
					wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), true)),
					wasm.RefVal(wasm.Ref(true, wasm.DefinedHeap(nil), false)),
				} {
					if internalFunctionReferenceWithTypes(typ, true, dir) {
						t.Fatal("unsupported indexed type form", typ)
					}
				}
				if a := testing.AllocsPerRun(10, func() { _ = dir.function(0) }); a != 0 {
					t.Fatal("directory resolution allocation", a)
				}
			})
		}
	}
}

func TestReferenceIndexedTypesFrontendProofs(t *testing.T) {
	for _, form := range []byte{0x63, 0x64} {
		for _, shape := range []string{"local", "block", "select"} {
			t.Run(fmt.Sprintf("wrong-signature-form%x-%s", form, shape), func(t *testing.T) {
				m := referenceIndexedTypesModule(t, false, form, shape, 2)
				referenceIndexedFrontendRejected(t, m, wasm.ErrTypeMismatch)
			})
		}
	}
	for _, tc := range []struct {
		name string
		body []byte
		code wasm.ValidationErrorCode
	}{
		{"get-before-set", []byte{0x20, 0, 0x1a, 0x0b}, wasm.ErrUninitializedLocal},
		{"nullable-assignment", []byte{0xd0, 0xc0, 0, 0x21, 0, 0x0b}, wasm.ErrTypeMismatch},
		{"nullable-join", []byte{2, 0x64, 0xc0, 0, 0xd0, 0xc0, 0, 0x0b, 0x1a, 0x0b}, wasm.ErrTypeMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := referenceIndexedTypesModule(t, false, 0x64, "local", 0)
			m.Code[3].BodyBytes = tc.body
			referenceIndexedFrontendRejected(t, m, tc.code)
		})
	}
}

func referenceIndexedFrontendRejected(t *testing.T, m *wasm.Module, code wasm.ValidationErrorCode) {
	t.Helper()
	for _, vector := range []bool{false, true} {
		if vector {
			m.Types[3].SubTypes[0].Comp.Params = []wasm.ValType{wasm.V128}
		}
		var v *wasm.ValidationError
		if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); !errors.As(err, &v) || v.Code != code {
			t.Fatal("indexed frontend proof", vector, err)
		}
		pressure := 0
		image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
		if image != nil || !errors.As(err, &v) || v.Code != code || pressure != 0 {
			t.Fatal("frontend proof before carrier erasure/staging", vector, image, err, pressure)
		}
	}
}

func TestReferenceIndexedTypesBoundsAndImmediateForms(t *testing.T) {
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	for _, vector := range []bool{false, true} {
		limit := 256
		if vector {
			limit = maxSWARLocals
		}
		for _, arity := range []int{limit, limit + 1} {
			for _, count := range []int{MaxFunctions, MaxFunctions + 1} {
				for _, result := range []bool{false, true} {
					sig := &wasm.CompType{Kind: wasm.CompFunc}
					values := make([]wasm.ValType, arity)
					for i := range values {
						values[i] = wasm.I32
					}
					if result {
						sig.Results = values
					} else {
						sig.Params = values
					}
					dir := internalReferenceTypes{scalar: make([]functionSpec, count)}
					if vector {
						dir = internalReferenceTypes{swar: make([]*wasm.CompType, count)}
						dir.swar[count-1] = sig
					} else {
						dir.scalar[count-1].signature = sig
					}
					if got := dir.function(uint32(count - 1)); got != (arity == limit && count == MaxFunctions) {
						t.Fatal("directory/profile boundary", vector, arity, count, result, got)
					}
					decoded := make([]wasm.RecType, count)
					decoded[count-1].SubTypes = []wasm.SubType{{Final: true, Comp: *sig}}
					if got := (internalReferenceTypes{decoded: decoded, decodedSWAR: vector}).function(uint32(count - 1)); got != (arity == limit && count == MaxFunctions) {
						t.Fatal("decoded directory/profile boundary", vector, arity, count, result, got)
					}
				}
			}
		}
	}
	for _, dir := range []internalReferenceTypes{{}, {scalar: []functionSpec{}}, {swar: []*wasm.CompType{}}, {scalar: []functionSpec{{signature: ft}}, swar: []*wasm.CompType{ft}}} {
		if dir.function(0) {
			t.Fatal("empty or mixed directory admitted")
		}
	}
	dir := internalReferenceTypes{scalar: []functionSpec{{signature: ft}}}
	for _, form := range []byte{0x63, 0x64} {
		for _, immediate := range [][]byte{{1}, wasmtest.SLEB64(0xffffffff), {0x62, 0}, {0x6f}, {0x80}} {
			r := wasm.ReaderFrom(append([]byte{form}, immediate...))
			if _, err := internalReferenceBlockType(&r, true, dir); err == nil {
				t.Fatal("invalid indexed block heap", form, immediate)
			}
			r = wasm.ReaderFrom(append([]byte{form}, immediate...))
			if _, err := internalSelectType(&r, true, dir); err == nil {
				t.Fatal("invalid indexed select heap", form, immediate)
			}
		}
	}
	// A bare block type index stays an indexed numeric signature, not a
	// reference result. Its meaning must not be changed by this parser.
	r := wasm.ReaderFrom(wasmtest.SLEB32(64))
	if got, err := internalReferenceBlockType(&r, true, dir); got != 64 || err != nil || r.HasNext() {
		t.Fatal("ordinary indexed block changed", got, err)
	}
	for _, vector := range []bool{false, true} {
		m := referenceIndexedTypesModule(t, vector, 0x63, "block", 0)
		m.Types[64].SubTypes[0].Comp.Results = []wasm.ValType{wasm.FuncRef}
		m.Code[3].BodyBytes = []byte{2, 0xc0, 0, 0xd2, 0, 0x0b, 0x1a, 0x0b}
		if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
			t.Fatal("reference-valued indexed block fixture", err)
		}
		pressure := 0
		image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
		if image == nil || err != nil || image.NativeReferenceControl || image.NativeReferenceCalls || image.CodeImage != nil || pressure != 1 {
			t.Fatal("baseline reference-valued indexed signature", err, pressure)
		}
	}
}

func TestReferenceIndexedTypesDecodedMetadata(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			mutate func(*wasm.RecType)
		}{
			{"empty-group", func(r *wasm.RecType) { r.SubTypes = nil }},
			{"recursive-group", func(r *wasm.RecType) { r.SubTypes = append(r.SubTypes, r.SubTypes[0]) }},
			{"nonfinal", func(r *wasm.RecType) { r.SubTypes[0].Final = false }},
			{"prefix", func(r *wasm.RecType) { r.SubTypes[0].HasPrefix = true }},
			{"super", func(r *wasm.RecType) { r.SubTypes[0].Supers = []wasm.TypeIdx{{Index: 0}} }},
			{"describes", func(r *wasm.RecType) { r.SubTypes[0].Metadata.Describes = wasm.SomeTypeIdx(wasm.TypeIdx{}) }},
			{"descriptor", func(r *wasm.RecType) { r.SubTypes[0].Metadata.Descriptor = wasm.SomeTypeIdx(wasm.TypeIdx{}) }},
			{"struct", func(r *wasm.RecType) { r.SubTypes[0].Comp.Kind = wasm.CompStruct }},
			{"reference-param", func(r *wasm.RecType) { r.SubTypes[0].Comp.Params = []wasm.ValType{wasm.FuncRef} }},
			{"reference-result", func(r *wasm.RecType) { r.SubTypes[0].Comp.Results = []wasm.ValType{wasm.FuncRef} }},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, tc.name), func(t *testing.T) {
				m := referenceIndexedTypesModule(t, vector, 0x63, "local", 0)
				tc.mutate(&m.Types[64])
				if (internalReferenceTypes{decoded: m.Types, decodedSWAR: vector}).function(64) {
					t.Fatal("unsupported decoded reference type", tc.name)
				}
				pressure := 0
				image, err := CompileModuleWith(m, CompileOptions{NativeReferenceCalls: true, ForeignTableCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, MemoryPressure: func() { pressure++ }})
				if image != nil || err == nil || pressure != 0 {
					t.Fatal("invalid decoded annotation type staged", image, err, pressure)
				}
			})
		}
	}
	ft := &wasm.CompType{Kind: wasm.CompFunc}
	decoded := []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: *ft}}}}
	for _, dir := range []internalReferenceTypes{
		{decoded: []wasm.RecType{}},
		{scalar: []functionSpec{{signature: ft}}, decoded: decoded},
		{swar: []*wasm.CompType{ft}, decoded: decoded},
		{scalar: []functionSpec{{signature: ft}}, decodedSWAR: true},
		{swar: []*wasm.CompType{ft}, decodedSWAR: true},
	} {
		if dir.function(0) {
			t.Fatal("mixed decoded view")
		}
	}
	dir := internalReferenceTypes{decoded: decoded}
	if a := testing.AllocsPerRun(10, func() {
		if !dir.function(0) {
			panic("decoded function directory")
		}
	}); a != 0 {
		t.Fatal("decoded directory allocation", a)
	}
	for _, prefix := range []byte{0, 0x70, 0x62} {
		r := wasm.ReaderFrom([]byte{prefix, 0})
		if _, err := internalExplicitReferenceType(&r, true, dir); err == nil {
			t.Fatal("explicit parser accepted another form", prefix)
		}
	}
}
