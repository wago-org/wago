package wasm

import (
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
)

func TestPrimitiveBinaryEdgeMatrix(t *testing.T) {
	t.Run("u33 and s33 expanded boundary tables", func(t *testing.T) {
		u33Cases := []struct {
			bytes []byte
			want  uint64
		}{
			{[]byte{0x00}, 0},
			{[]byte{0x01}, 1},
			{[]byte{0x7f}, 127},
			{[]byte{0x80, 0x01}, 128},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xffffffff},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x1f}, 0x1ffffffff},
		}
		for _, tc := range u33Cases {
			got, err := newReader(tc.bytes).u33()
			if err != nil || got != tc.want {
				t.Fatalf("u33 %x = %#x err=%v", tc.bytes, got, err)
			}
		}
		s33Cases := []struct {
			bytes []byte
			want  int64
		}{
			{[]byte{0x00}, 0},
			{[]byte{0x01}, 1},
			{[]byte{0x7f}, -1},
			{[]byte{0x3f}, 63},
			{[]byte{0x40}, -64},
			{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, 0xffffffff},
			{[]byte{0x80, 0x80, 0x80, 0x80, 0x70}, -0x100000000},
		}
		for _, tc := range s33Cases {
			got, err := newReader(tc.bytes).s33()
			if err != nil || got != tc.want {
				t.Fatalf("s33 %x = %#x err=%v", tc.bytes, got, err)
			}
		}
	})
	t.Run("invalid decode vectors", func(t *testing.T) {
		vectors := []struct {
			name string
			data []byte
			code DecodeErrorCode
		}{
			{"bad magic", []byte{0x01, 0x61, 0x73, 0x6d, 0x01, 0, 0, 0}, ErrBadMagic},
			{"bad version", []byte{0x00, 0x61, 0x73, 0x6d, 0x02, 0, 0, 0}, ErrBadVersion},
			{"payload overflow", []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0, 0, 0, secCustom, 0x02, 0x00}, ErrIndexOutOfBounds},
			{"invalid type payload", module(section(secType, 0x01, 0xff)), ErrInvalidType},
			{"invalid opcode", module(section(secType, 0x01, 0x60, 0, 0), section(secFunction, 1, 0), section(secCode, 1, 3, 0, 0xff, 0x0b)), ErrInvalidInstruction},
		}
		for _, tc := range vectors {
			_, err := DecodeModule(tc.data)
			var de *DecodeError
			if !errors.As(err, &de) || de.Code != tc.code {
				t.Fatalf("%s: expected %v, got %v", tc.name, tc.code, err)
			}
		}
	})
	t.Run("generic list decode propagates nested failures", func(t *testing.T) {
		decodeBool := func(r *reader) (bool, error) {
			b, err := r.byte()
			if err != nil {
				return false, err
			}
			if b > 1 {
				return false, &DecodeError{Code: ErrInvalidType, Offset: r.off() - 1}
			}
			return b == 1, nil
		}
		if _, err := readVec(newReader(nil), decodeBool); err == nil {
			t.Fatal("expected vector length EOF")
		}
		if _, err := readVec(newReader([]byte{1, 2}), decodeBool); err == nil {
			t.Fatal("expected nested bool failure")
		}
		got, err := readVec(newReader([]byte{2, 0, 1}), decodeBool)
		if err != nil || len(got) != 2 || got[0] || !got[1] {
			t.Fatalf("bool vec=%v err=%v", got, err)
		}
	})
	t.Run("fixed width float payload preservation", func(t *testing.T) {
		f32Payload := []byte{0x43, 0x01, 0x00, 0xc0, 0x7f}
		in, err := decodeInstruction(newReader(f32Payload), 0)
		if err != nil || in.F32Bits != 0x7fc00001 {
			t.Fatalf("f32 const bits=%#x err=%v", in.F32Bits, err)
		}
		f64Payload := []byte{0x44, 0x01, 0, 0, 0, 0, 0, 0xf8, 0x7f}
		in, err = decodeInstruction(newReader(f64Payload), 0)
		if err != nil || in.F64Bits != 0x7ff8000000000001 {
			t.Fatalf("f64 const bits=%#x err=%v", in.F64Bits, err)
		}
	})
	t.Run("start code data-count wrapper edge cases", func(t *testing.T) {
		if m, err := DecodeModule(module(section(secType, 0), section(secStart, 0))); err != nil || m.Start == nil || *m.Start != 0 {
			t.Fatalf("start wrapper failed m=%#v err=%v", m, err)
		}
		if m, err := DecodeModule(module(section(secDataCount, 0))); err != nil || m.DataCount == nil || *m.DataCount != 0 {
			t.Fatalf("data count wrapper failed m=%#v err=%v", m, err)
		}
		_, err := DecodeModule(module([]byte{secStart, 0x00}))
		if err == nil {
			t.Fatal("expected legacy non-length start section failure")
		}
		_, err = DecodeModule(module([]byte{secDataCount, 0x00}))
		if err == nil {
			t.Fatal("expected legacy non-length data count section failure")
		}
	})
}

func TestScalarSignExtensionInstructionDecodeMatrix(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		kind  InstrKind
	}{
		{"i32.extend8_s", []byte{0xc0}, InstrI32Extend8S},
		{"i32.extend16_s", []byte{0xc1}, InstrI32Extend16S},
		{"i64.extend8_s", []byte{0xc2}, InstrI64Extend8S},
		{"i64.extend16_s", []byte{0xc3}, InstrI64Extend16S},
		{"i64.extend32_s", []byte{0xc4}, InstrI64Extend32S},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := decodeInstruction(newReader(tc.bytes), 0)
			if signExtensionBuilt == 0 {
				checkDisabledInstructionDecode(t, err, 0, "sign-extension-ops")
				typ, literal := I32, byte(0x41)
				if tc.kind >= InstrI64Extend8S {
					typ, literal = I64, 0x42
				}
				for _, withFacts := range []bool{false, true} {
					m := modWithFunc(nil, []ValType{typ})
					m.Code[0].BodyBytes = []byte{literal, 0, tc.bytes[0], 0x0b}
					if withFacts {
						var analysis ValidatedModuleAnalysis
						err = ValidateModuleWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{}, &analysis)
					} else {
						err = ValidateModule(m)
					}
					checkDisabledInstructionDecode(t, err, 2, "sign-extension-ops")
				}
				expectValidateErr(t, modWithFunc(nil, []ValType{typ},
					Instruction{Kind: constFor(typ).Kind}, Instruction{Kind: tc.kind}), ErrUnsupportedFeature)
				return
			}
			if err != nil || in.Kind != tc.kind {
				t.Fatalf("decode %x = %#v err=%v, want %v", tc.bytes, in.Kind, err, tc.kind)
			}
		})
	}
}

func checkDisabledInstructionDecode(t *testing.T, err error, offset int, feature string) {
	t.Helper()
	var decoded *DecodeError
	if !errors.As(err, &decoded) || decoded.Code != ErrDisabledInstruction || decoded.Offset != offset ||
		!strings.Contains(err.Error(), feature+" is disabled in this build") {
		t.Fatalf("decode error = %v, want disabled %s at byte %d", err, feature, offset)
	}
}

func TestCallInstructionBuildFeatures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    []byte
		kind    InstrKind
		feature string
	}{
		{"call", []byte{0x10, 0}, InstrCall, ""},
		{"call_indirect", []byte{0x11, 0, 0}, InstrCallIndirect, ""},
		{"return_call", []byte{0x12, 0}, InstrReturnCall, ""},
		{"return_call_indirect", []byte{0x13, 0, 0}, InstrReturnCallIndirect, ""},
		{"call_ref", []byte{0x14, 0}, InstrCallRef, "typed-function-references"},
		{"return_call_ref", []byte{0x15, 0}, InstrReturnCallRef, "typed-function-references"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disabled := ""
			switch {
			case buildfeatures.TailCall == 0 && (tc.kind == InstrReturnCall || tc.kind == InstrReturnCallIndirect || tc.kind == InstrReturnCallRef):
				disabled = "tail-call"
			case buildfeatures.TypedFunctionReferences == 0:
				disabled = tc.feature
			}
			in, err := decodeInstruction(newReader(tc.code), 0)
			reader := NewReader(tc.code)
			_, _ = reader.Byte()
			immediate, classifyErr := ClassifyInstructionImmediate(reader, tc.code[0])
			reader = NewReader(tc.code)
			_, _ = reader.Byte()
			skipErr := SkipInstructionImmediate(reader, tc.code[0])
			if disabled == "" {
				if err != nil || in.Kind != tc.kind {
					t.Fatalf("decode %x = %s, %v, want %s", tc.code, in.Kind, err, tc.kind)
				}
				if classifyErr != nil || immediate.Kind != tc.kind || skipErr != nil || reader.HasNext() {
					t.Fatalf("classify or skip %x = %v, %v, %v", tc.code, immediate.Kind, classifyErr, skipErr)
				}
				return
			}
			checkDisabledInstructionDecode(t, err, 0, disabled)
			checkDisabledInstructionDecode(t, classifyErr, 0, disabled)
			checkDisabledInstructionDecode(t, skipErr, 0, disabled)
			for _, withFacts := range []bool{false, true} {
				m := modWithFunc(nil, nil)
				m.Code[0].BodyBytes = append(append([]byte(nil), tc.code...), 0x0b)
				if withFacts {
					var analysis ValidatedModuleAnalysis
					err = ValidateModuleWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{}, &analysis)
				} else {
					err = ValidateModule(m)
				}
				checkDisabledInstructionDecode(t, err, 0, disabled)
			}
			m := modWithFunc(nil, nil, Instruction{Kind: tc.kind})
			var validation *ValidationError
			err = ValidateModule(m)
			if !errors.As(err, &validation) || validation.Code != ErrUnsupportedFeature ||
				!strings.Contains(validation.Detail, disabled) {
				t.Fatalf("validate %s = %v, want unsupported %s", tc.kind, err, disabled)
			}
		})
	}
}

func TestScalarTruncSatInstructionDecodeMatrix(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		kind  InstrKind
	}{
		{"i32.trunc_sat_f32_s", []byte{0xfc, 0x00}, InstrI32TruncSatF32S},
		{"i32.trunc_sat_f32_u", []byte{0xfc, 0x01}, InstrI32TruncSatF32U},
		{"i32.trunc_sat_f64_s", []byte{0xfc, 0x02}, InstrI32TruncSatF64S},
		{"i32.trunc_sat_f64_u", []byte{0xfc, 0x03}, InstrI32TruncSatF64U},
		{"i64.trunc_sat_f32_s", []byte{0xfc, 0x04}, InstrI64TruncSatF32S},
		{"i64.trunc_sat_f32_u", []byte{0xfc, 0x05}, InstrI64TruncSatF32U},
		{"i64.trunc_sat_f64_s", []byte{0xfc, 0x06}, InstrI64TruncSatF64S},
		{"i64.trunc_sat_f64_u", []byte{0xfc, 0x07}, InstrI64TruncSatF64U},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, err := decodeInstruction(newReader(tc.bytes), 0)
			if buildfeatures.NonTrappingFloatToIntConversion == 0 {
				checkDisabledInstructionDecode(t, err, len(tc.bytes), "nontrapping-float-to-int-conversion")
				return
			}
			if err != nil || in.Kind != tc.kind {
				t.Fatalf("decode %x = %#v err=%v, want %v", tc.bytes, in.Kind, err, tc.kind)
			}
		})
	}
}

func TestMultiValueBlockTypeDecodeAndValidation(t *testing.T) {
	if buildfeatures.MultiValue == 0 {
		_, err := decodeBlockType(newReader([]byte{0}))
		checkDisabledInstructionDecode(t, err, 0, "multi-value")
		for _, encoded := range [][]byte{{0x40}, {0x7f}} {
			if _, err := decodeBlockType(newReader(encoded)); err != nil {
				t.Fatalf("ordinary block type %x: %v", encoded, err)
			}
		}
		return
	}
	multiResultFuncType := []byte{0x60, 0x00, 0x02, 0x7f, 0x7e} // () -> (i32, i64)
	singleResultFuncType := []byte{0x60, 0x00, 0x01, 0x7f}      // () -> i32
	typePayload := append([]byte{0x03}, multiResultFuncType...)
	typePayload = append(typePayload, multiResultFuncType...)
	typePayload = append(typePayload, singleResultFuncType...)
	moduleWithBody := func(body []byte) []byte {
		codePayload := append([]byte{0x01}, u32(uint32(len(body)))...)
		codePayload = append(codePayload, body...)
		return module(
			section(secType, typePayload...),
			section(secFunction, 0x01, 0x01),
			section(secCode, codePayload...),
		)
	}

	validBlockBody := []byte{
		0x00,       // local decl count
		0x02, 0x00, // block typeidx 0: () -> (i32, i64)
		0x41, 0x07, // i32.const 7
		0x42, 0x09, // i64.const 9
		0x0b, // end block
		0x0b, // end func
	}
	m, err := DecodeModule(moduleWithBody(validBlockBody))
	if err != nil {
		t.Fatalf("DecodeModule multi-value block typeidx: %v", err)
	}
	if len(m.Code) != 1 || !sameValTypes(m.Types[0].SubTypes[0].Comp.Results, []ValType{I32, I64}) {
		t.Fatalf("decoded multi-value function type = %#v", m.Types)
	}
	if got, want := m.Code[0].BodyBytes, validBlockBody[1:]; string(got) != string(want) {
		t.Fatalf("body bytes=%x want %x", got, want)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule multi-value block typeidx: %v", err)
	}

	validBranchBody := []byte{
		0x00,       // local decl count
		0x02, 0x00, // block typeidx 0: () -> (i32, i64)
		0x41, 0x15, // i32.const 21
		0x42, 0x16, // i64.const 22
		0x41, 0x01, // i32.const 1
		0x0d, 0x00, // br_if 0 carrying both block results
		0x1a,       // fallthrough: drop i64
		0x1a,       // fallthrough: drop i32
		0x41, 0x1f, // i32.const 31
		0x42, 0x20, // i64.const 32
		0x0b, // end block
		0x0b, // end func
	}
	m, err = DecodeModule(moduleWithBody(validBranchBody))
	if err != nil {
		t.Fatalf("DecodeModule multi-value br_if payload: %v", err)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule multi-value br_if payload: %v", err)
	}

	validBrTableBody := []byte{
		0x00,       // local decl count
		0x02, 0x00, // outer block typeidx 0: () -> (i32, i64)
		0x02, 0x00, // inner block typeidx 0: () -> (i32, i64)
		0x41, 0x0b, // i32.const 11
		0x42, 0x0c, // i64.const 12
		0x41, 0x00, // selector
		0x0e, 0x01, 0x00, 0x01, // br_table [inner] default outer, carrying both results
		0x0b,       // end inner
		0x1a,       // drop i64 payload from the selected inner branch
		0x1a,       // drop i32 payload from the selected inner branch
		0x41, 0x0d, // i32.const 13
		0x42, 0x0e, // i64.const 14
		0x0b, // end outer
		0x0b, // end func
	}
	m, err = DecodeModule(moduleWithBody(validBrTableBody))
	if err != nil {
		t.Fatalf("DecodeModule multi-value br_table payload: %v", err)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatalf("ValidateModule multi-value br_table payload: %v", err)
	}

	invalidBrTableLabelBody := []byte{
		0x00,       // local decl count
		0x02, 0x00, // outer block typeidx 0: () -> (i32, i64)
		0x02, 0x02, // inner block typeidx 2: () -> i32
		0x41, 0x0b, // i32.const 11
		0x42, 0x0c, // i64.const 12
		0x41, 0x00, // selector
		0x0e, 0x01, 0x00, 0x01, // target inner has a different label arity than default outer
		0x0b, // end inner
		0x0b, // end outer
		0x0b, // end func
	}
	m, err = DecodeModule(moduleWithBody(invalidBrTableLabelBody))
	if err != nil {
		t.Fatalf("DecodeModule invalid multi-value br_table labels: %v", err)
	}
	expectValidateErr(t, m, ErrTypeMismatch)

	invalidBranchBody := []byte{
		0x00,       // local decl count
		0x02, 0x00, // block typeidx 0: () -> (i32, i64)
		0x41, 0x07, // i32.const 7; missing i64 branch payload
		0x0c, 0x00, // br 0
		0x0b, // end block
		0x0b, // end func
	}
	m, err = DecodeModule(moduleWithBody(invalidBranchBody))
	if err != nil {
		t.Fatalf("DecodeModule invalid multi-value branch payload: %v", err)
	}
	expectValidateErr(t, m, ErrTypeMismatch)
}

func TestSIMDAndRelaxedSIMDDecodeMatrix(t *testing.T) {
	cases := []struct {
		sub  uint32
		kind InstrKind
	}{
		{55, InstrI32x4Eq}, {64, InstrI32x4GeU}, {65, InstrF32x4Eq}, {76, InstrF64x2Ge},
		{77, InstrV128Not}, {82, InstrV128Bitselect}, {94, InstrF32x4DemoteF64x2Zero},
		{96, InstrI8x16Abs}, {127, InstrI32x4ExtaddPairwiseI16x8U}, {128, InstrI16x8Abs},
		{160, InstrI32x4Abs}, {192, InstrI64x2Abs}, {224, InstrF32x4Abs}, {255, InstrF64x2ConvertLowI32x4U},
		{256, InstrI8x16RelaxedSwizzle}, {257, InstrI32x4RelaxedTruncF32x4S}, {258, InstrI32x4RelaxedTruncF32x4U},
		{259, InstrI32x4RelaxedTruncZeroF64x2S}, {260, InstrI32x4RelaxedTruncZeroF64x2U},
		{261, InstrF32x4RelaxedMadd}, {262, InstrF32x4RelaxedNmadd}, {263, InstrF64x2RelaxedMadd}, {264, InstrF64x2RelaxedNmadd},
		{265, InstrI8x16RelaxedLaneselect}, {266, InstrI16x8RelaxedLaneselect}, {267, InstrI32x4RelaxedLaneselect}, {268, InstrI64x2RelaxedLaneselect},
		{269, InstrF32x4RelaxedMin}, {270, InstrF32x4RelaxedMax}, {271, InstrF64x2RelaxedMin}, {272, InstrF64x2RelaxedMax},
		{273, InstrI16x8RelaxedQ15mulrS}, {274, InstrI16x8RelaxedDotI8x16I7x16S}, {275, InstrI32x4RelaxedDotI8x16I7x16AddS},
	}
	for _, tc := range cases {
		b := append([]byte{0xfd}, u32(tc.sub)...)
		in, err := decodeInstruction(newReader(b), 0)
		if buildfeatures.SIMD == 0 {
			checkDisabledInstructionDecode(t, err, 0, "simd")
			continue
		}
		if buildfeatures.RelaxedSIMD == 0 && tc.sub >= firstRelaxedSIMDSubopcode {
			checkDisabledInstructionDecode(t, err, len(b), "relaxed-simd")
			continue
		}
		if err != nil || in.Kind != tc.kind {
			t.Fatalf("sub %d decoded %#v err=%v", tc.sub, in.Kind, err)
		}
	}
}

func TestAtomicAndGCInstructionDecodeMatrix(t *testing.T) {
	atomicCases := []struct {
		bytes []byte
		kind  InstrKind
		op    uint32
	}{
		{[]byte{0xfe, 0x1e, 0, 0}, InstrAtomicRmw, 30},
		{[]byte{0xfe, 0x47, 0, 0}, InstrAtomicRmw, 71},
		{[]byte{0xfe, 0x48, 0, 0}, InstrAtomicCmpxchg, 72},
		{[]byte{0xfe, 0x4e, 0, 0}, InstrAtomicCmpxchg, 78},
	}
	for _, tc := range atomicCases {
		in, err := decodeInstruction(newReader(tc.bytes), 0)
		if err != nil || in.Kind != tc.kind || in.AtomicOp != tc.op {
			t.Fatalf("%x -> %#v err=%v", tc.bytes, in, err)
		}
	}
	structAtomic := []struct {
		bytes []byte
		kind  InstrKind
		ord   AtomicOrder
		typeI uint32
		fld   uint32
	}{
		{[]byte{0xfe, 0x5c, 0x01, 0x00, 0x00}, InstrStructAtomicGet, AcqRel, 0, 0},
		{[]byte{0xfe, 0x5d, 0x00, 0x01, 0x02}, InstrStructAtomicGetS, SeqCst, 1, 2},
		{[]byte{0xfe, 0x5e, 0x01, 0x02, 0x03}, InstrStructAtomicGetU, AcqRel, 2, 3},
	}
	for _, tc := range structAtomic {
		in, err := decodeInstruction(newReader(tc.bytes), 0)
		if err != nil || in.Kind != tc.kind || in.AtomicOrder != tc.ord || in.Index != tc.typeI || in.Index2 != tc.fld {
			t.Fatalf("%x -> %#v err=%v", tc.bytes, in, err)
		}
	}
	if _, err := decodeInstruction(newReader([]byte{0xfe, 0x5c, 0x02, 0x00, 0x00}), 0); err == nil {
		t.Fatal("expected invalid struct atomic order")
	}
}

func TestCustomSectionNameDetailSpan(t *testing.T) {
	bad := module(section(secCustom, 0x01, 0xff))
	_, err := DecodeModule(bad)
	var de *DecodeError
	if !errors.As(err, &de) || de.Code != ErrInvalidSection || de.SectionID != secCustom || de.SectionStart != 10 || de.SectionEnd != len(bad) {
		t.Fatalf("expected malformed custom-section name with section span, got %#v / %v", de, err)
	}
}

func TestExceptionInstructionBuildFeature(t *testing.T) {
	for _, tc := range []struct {
		name string
		code []byte
		kind InstrKind
	}{
		{"throw", []byte{0x08, 0}, InstrThrow},
		{"throw_ref", []byte{0x0a}, InstrThrowRef},
		{"try_table", []byte{0x1f, 0x40, 0, 0x0b}, InstrTryTable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := decodeInstruction(newReader(tc.code), 0)
			if buildfeatures.ExceptionHandling != 0 {
				if err != nil || in.Kind != tc.kind {
					t.Fatalf("decode %x = %s, %v, want %s", tc.code, in.Kind, err, tc.kind)
				}
				return
			}
			checkDisabledInstructionDecode(t, err, 0, "exception-handling")
			// The opcode alone must be enough to reject the instruction.
			_, err = decodeInstruction(newReader(tc.code[:1]), 0)
			checkDisabledInstructionDecode(t, err, 0, "exception-handling")
			for _, withFacts := range []bool{false, true} {
				m := modWithFunc(nil, nil)
				m.Code[0].BodyBytes = tc.code[:1]
				if withFacts {
					var analysis ValidatedModuleAnalysis
					err = ValidateModuleWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{}, &analysis)
				} else {
					err = ValidateModule(m)
				}
				checkDisabledInstructionDecode(t, err, 0, "exception-handling")
			}
			expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: tc.kind}), ErrUnsupportedFeature)
		})
	}
}

func TestExceptionTagBuildFeature(t *testing.T) {
	if buildfeatures.ExceptionHandling != 0 {
		t.Skip("requires a build without exception handling")
	}
	_, err := decodeExternType(newReader([]byte{byte(ExternTag)}))
	checkDisabledInstructionDecode(t, err, 1, "exception-handling")

	defined := modWithFunc(nil, nil)
	defined.Tags = []TagType{{Type: TypeIdx{Index: 0}}}
	expectValidateErr(t, defined, ErrUnsupportedFeature)

	imported := modWithFunc(nil, nil)
	imported.Imports = []Import{{Type: NewTagExternType(TagType{Type: TypeIdx{Index: 0}})}}
	expectValidateErr(t, imported, ErrUnsupportedFeature)
}

func TestReferenceValueBuildFeature(t *testing.T) {
	for _, tc := range []struct {
		code    []byte
		kind    InstrKind
		feature string
		enabled bool
	}{
		{[]byte{0x25, 0}, InstrTableGet, "reference-types", buildfeatures.ReferenceSignatures},
		{[]byte{0x26, 0}, InstrTableSet, "reference-types", buildfeatures.ReferenceSignatures},
		{[]byte{0xd0, 0x70}, InstrRefNull, "reference-types", buildfeatures.ReferenceSignatures},
		{[]byte{0xd1}, InstrRefIsNull, "reference-types", buildfeatures.ReferenceSignatures},
		{[]byte{0xd2, 0}, InstrRefFunc, "reference-types", buildfeatures.ReferenceSignatures},
		{[]byte{0xd3}, InstrRefEq, "gc", buildfeatures.GC != 0},
		{[]byte{0xd4}, InstrRefAsNonNull, "typed-function-references", buildfeatures.TypedFunctionReferences != 0},
		{[]byte{0xd5, 0}, InstrBrOnNull, "typed-function-references", buildfeatures.TypedFunctionReferences != 0},
		{[]byte{0xd6, 0}, InstrBrOnNonNull, "typed-function-references", buildfeatures.TypedFunctionReferences != 0},
	} {
		in, err := decodeInstruction(newReader(tc.code), 0)
		if tc.enabled {
			if err != nil || in.Kind != tc.kind {
				t.Fatalf("decode %x = %s, %v, want %s", tc.code, in.Kind, err, tc.kind)
			}
			continue
		}
		checkDisabledInstructionDecode(t, err, 0, tc.feature)
		if tc.feature != "reference-types" {
			_, err := skipExprOp(newReader(tc.code))
			checkDisabledInstructionDecode(t, err, 0, tc.feature)
		}
		m := modWithFunc(nil, nil)
		m.Code[0].BodyBytes = append(append([]byte(nil), tc.code...), 0x0b)
		checkDisabledInstructionDecode(t, ValidateModule(m), 0, tc.feature)
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: tc.kind}), ErrUnsupportedFeature)
	}
	if buildfeatures.GC == 0 {
		expectValidateErr(t, modWithFunc(nil, nil, Instruction{Kind: InstrStringConst}), ErrUnsupportedFeature)
	}
	if !buildfeatures.ReferenceSignatures {
		_, err := decodeValType(newReader([]byte{0x70}))
		checkDisabledInstructionDecode(t, err, 0, "reference-types")
		var elem directElem
		err = decodeDirectElem(newReader([]byte{0x07}), &elem)
		checkDisabledInstructionDecode(t, err, 0, "reference-types")
	}
}

func TestMVPTableBuildFeature(t *testing.T) {
	m, err := DecodeModule(module(section(secTable, 1, 0x70, 0, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if !buildfeatures.ReferenceSignatures {
		_, err := DecodeModule(module(section(secTable, 1, 0x6f, 0, 1)))
		checkDisabledInstructionDecode(t, err, 11, "reference-types")
		m.Tables[0].Type.Ref = AbsRef(HeapExtern)
		expectValidateErr(t, m, ErrUnsupportedFeature)
	}
}
