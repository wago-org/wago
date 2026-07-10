package wasm

import (
	"errors"
	"testing"
)

func TestDecodeLocalsRejectsAggregateCountOverflow(t *testing.T) {
	for _, tc := range []struct {
		name string
		runs []LocalRun
	}{
		{
			name: "max-plus-one",
			runs: []LocalRun{{Count: ^uint32(0), Type: I32}, {Count: 1, Type: I64}},
		},
		{
			name: "four-quarter-runs",
			runs: []LocalRun{
				{Count: 1 << 30, Type: I32},
				{Count: 1 << 30, Type: I64},
				{Count: 1 << 30, Type: F32},
				{Count: 1 << 30, Type: F64},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := localRunsBody(tc.runs)
			data := localRunsModule(nil, body)
			for _, path := range []struct {
				name   string
				decode func([]byte) error
			}{
				{name: "AST", decode: func(b []byte) error { _, err := decodeModuleASTForTest(b); return err }},
				{name: "DecodeModule", decode: func(b []byte) error { _, err := DecodeModule(b); return err }},
				{name: "DecodeModuleByteBacked", decode: func(b []byte) error { _, err := DecodeModuleByteBacked(b); return err }},
			} {
				t.Run(path.name, func(t *testing.T) {
					err := path.decode(data)
					var de *DecodeError
					if !errors.As(err, &de) || de.Code != ErrInvalidModule {
						t.Fatalf("decode error=%#v / %v, want ErrInvalidModule", de, err)
					}
					if de.SectionID != secCode || de.SectionStart <= 0 || de.SectionEnd <= de.SectionStart {
						t.Fatalf("decode section diagnostics=%#v, want code-section span", de)
					}
				})
			}

			codePayload := append([]byte{0x01}, u32(uint32(len(body)))...)
			codePayload = append(codePayload, body...)
			if _, _, err := decodeDirectCodeSection(newReader(codePayload), false); err == nil {
				t.Fatal("decodeDirectCodeSection accepted aggregate local-count overflow")
			}
		})
	}
}

func TestDecodeLocalsPreservesUint32BoundaryAndZeroRuns(t *testing.T) {
	runs := []LocalRun{
		{Count: 0, Type: I32},
		{Count: ^uint32(0), Type: I64},
		{Count: 0, Type: F32},
	}
	body := localRunsBody(runs)
	data := localRunsModule([]ValType{I32, I64}, body)

	ast, err := decodeModuleASTForTest(data)
	if err != nil {
		t.Fatalf("AST decode rejected boundary local count: %v", err)
	}
	assertLocalRunsEqual(t, ast.Code[0].Locals.Runs, runs)

	m, err := DecodeModule(data)
	if err != nil {
		t.Fatalf("DecodeModule rejected boundary local count: %v", err)
	}
	assertLocalRunsEqual(t, m.Code[0].Locals.Runs, runs)

	dm, err := DecodeModuleByteBacked(data)
	if err != nil {
		t.Fatalf("DecodeModuleByteBacked rejected boundary local count: %v", err)
	}
	assertLocalRunsEqual(t, dm.Module.Code[0].Locals.Runs, runs)

	codePayload := append([]byte{0x01}, u32(uint32(len(body)))...)
	codePayload = append(codePayload, body...)
	funcs, _, err := decodeDirectCodeSection(newReader(codePayload), false)
	if err != nil {
		t.Fatalf("decodeDirectCodeSection rejected boundary local count: %v", err)
	}
	assertLocalRunsEqual(t, funcs[0].Locals.Runs, runs)
}

func assertLocalRunsEqual(t *testing.T, got, want []LocalRun) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("local runs=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("local run %d=%v, want %v", i, got[i], want[i])
		}
	}
}

func localRunsModule(params []ValType, body []byte) []byte {
	typePayload := []byte{0x01, 0x60}
	typePayload = append(typePayload, u32(uint32(len(params)))...)
	for _, param := range params {
		typePayload = append(typePayload, MustEncodeValType(param))
	}
	typePayload = append(typePayload, 0x00)
	codePayload := append([]byte{0x01}, u32(uint32(len(body)))...)
	codePayload = append(codePayload, body...)
	return module(
		section(secType, typePayload...),
		section(secFunction, 0x01, 0x00),
		section(secCode, codePayload...),
	)
}

func localRunsBody(runs []LocalRun) []byte {
	body := u32(uint32(len(runs)))
	for _, run := range runs {
		body = append(body, u32(run.Count)...)
		body = append(body, MustEncodeValType(run.Type))
	}
	return append(body, 0x0b)
}
