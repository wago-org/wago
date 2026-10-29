package wasm

import (
	"fmt"
	"testing"
)

func structValidationModule(fields, accesses int, opcode byte) []byte {
	types := append([]byte{2, 0x5f}, u32(uint32(fields))...)
	for range fields {
		types = append(types, 0x7f, 0x01) // (field (mut i32))
	}
	types = append(types, 0x60, 1, 0x63, 0, 0) // (func (param (ref null 0)))
	body := []byte{0}                          // no locals
	for range accesses {
		switch opcode {
		case 0: // struct.new
			for range fields {
				body = append(body, 0x41, 0) // i32.const 0
			}
		case 2, 5: // struct.get/set
			body = append(body, 0x20, 0) // local.get 0
			if opcode == 5 {
				body = append(body, 0x41, 0) // i32.const 0
			}
		}
		body = append(body, 0xfb, opcode, 0)
		if opcode == 2 || opcode == 5 {
			body = append(body, u32(uint32(fields-1))...)
		}
		if opcode != 5 {
			body = append(body, 0x1a) // drop
		}
	}
	body = append(body, 0x0b)
	code := append([]byte{1}, u32(uint32(len(body)))...)
	code = append(code, body...)
	return module(section(secType, types...), section(secFunction, 1, 1), section(secCode, code...))
}

func TestValidateStructFieldAccessAllocationGrowth(t *testing.T) {
	for _, op := range []struct {
		name   string
		opcode byte
	}{{"get", 2}, {"set", 5}, {"new", 0}, {"new_default", 1}} {
		t.Run(op.name, func(t *testing.T) {
			allocs := func(accesses int) float64 {
				m, err := DecodeModule(structValidationModule(512, accesses, op.opcode))
				if err != nil {
					t.Fatal(err)
				}
				return validationAllocs(t, m)
			}
			one, many := allocs(1), allocs(128)
			t.Logf("validation allocations: one access=%.0f, 128 accesses=%.0f", one, many)
			if many-one > 2 {
				t.Fatalf("struct field accesses add %.0f allocations; resolving a wide struct must not allocate per access", many-one)
			}
		})
	}
}

func TestValidateStructFieldsResolveRecursiveReferencesWithWorkers(t *testing.T) {
	// The recursion group starts at flat index 1. Its first member contains a
	// mutable reference to the second between scalar and packed fields. It must
	// resolve to flat index 2 for gets, sets, and constructors in every worker.
	data := module(
		section(secType, 6,
			0x60, 0, 0,
			0x4e, 2, 0x5f, 3, 0x7f, 1, 0x63, 2, 1, 0x77, 1, 0x5f, 0,
			0x60, 1, 0x63, 1, 1, 0x63, 2,
			0x60, 2, 0x63, 1, 0x63, 2, 0,
			0x60, 1, 0x63, 2, 1, 0x64, 1,
			0x60, 0, 1, 0x64, 1,
		),
		section(secFunction, 4, 3, 4, 5, 6),
		section(secCode, 4,
			8, 0, 0x20, 0, 0xfb, 2, 1, 1, 0x0b,
			10, 0, 0x20, 0, 0x20, 1, 0xfb, 5, 1, 1, 0x0b,
			11, 0, 0x41, 0, 0x20, 0, 0x41, 0, 0xfb, 0, 1, 0x0b,
			5, 0, 0xfb, 1, 1, 0x0b,
		),
	)
	for _, tree := range []bool{false, true} {
		t.Run(fmt.Sprintf("tree=%t", tree), func(t *testing.T) {
			decode := DecodeModule
			if tree {
				decode = decodeModuleASTForTest
			}
			m, err := decode(data)
			if err != nil {
				t.Fatal(err)
			}
			for range 5 {
				m.FuncTypes = append(m.FuncTypes, m.FuncTypes...)
				m.Code = append(m.Code, m.Code...)
			}
			fields := m.Types[1].SubTypes[0].Comp.Fields
			original := append([]FieldType(nil), fields...)
			for _, workers := range []int{1, 2, 8} {
				if err := ValidateModuleWithWorkers(m, workers); err != nil {
					t.Fatalf("workers=%d: %v", workers, err)
				}
				for i, f := range fields {
					if f != original[i] {
						t.Fatalf("workers=%d: validation changed field %d", workers, i)
					}
				}
			}
			heap := fields[1].Storage().Val().Ref().Heap()
			if got := heap.Type(); got != (TypeIdx{Index: 1, Rec: true}) {
				t.Fatalf("validation changed the original recursive field type: %+v", got)
			}
			// Revalidation must see edits to decoded fields, not a stale resolved
			// copy, and workers must report the same lowest-index invalid body.
			fields[1] = NewFieldType(fields[1].Storage(), Const)
			for _, workers := range []int{1, 2, 8} {
				err := ValidateModuleWithWorkers(m, workers)
				ve, ok := err.(*ValidationError)
				if !ok || *ve != (ValidationError{Code: ErrTypeMismatch, Func: 1, Detail: "immutable field"}) {
					t.Fatalf("workers=%d: error = %v, want immutable field in function 1", workers, err)
				}
			}
		})
	}
}

func TestValidateStructConstructorsCheckEveryField(t *testing.T) {
	for _, kind := range []InstrKind{InstrStructNew, InstrStructNewDefault} {
		for bad := range 3 {
			t.Run(fmt.Sprintf("%v/field=%d", kind, bad), func(t *testing.T) {
				fields := []FieldType{field(I32, Var), field(I32, Var), field(I32, Var)}
				body := []Instruction{{Kind: InstrI32Const}, {Kind: InstrI32Const}, {Kind: InstrI32Const}}
				fields[bad] = field(I64, Var)
				if kind == InstrStructNewDefault {
					fields[bad] = field(refToType(0, false), Var)
					body = nil
				}
				body = append(body, Instruction{Kind: kind, Index: 1}, Instruction{Kind: InstrDrop})
				m := &Module{Types: []RecType{ft(nil, nil), structType(fields, TypeMetadata{})}}
				for range 8 {
					m.FuncTypes = append(m.FuncTypes, TypeIdx{Index: 0})
					m.Code = append(m.Code, Func{Body: Expr{Instrs: body}})
				}
				for _, workers := range []int{1, 8} {
					err := ValidateModuleWithWorkers(m, workers)
					ve, ok := err.(*ValidationError)
					if !ok || ve.Code != ErrTypeMismatch || ve.Func != 0 {
						t.Fatalf("workers=%d: error = %v, want type mismatch in function 0", workers, err)
					}
				}
			})
		}
	}
}

func BenchmarkValidateStructFieldAccess(b *testing.B) {
	for _, fields := range []int{1, 64, 4096} {
		for _, accesses := range []int{1, 1024} {
			b.Run(fmt.Sprintf("fields=%d/accesses=%d", fields, accesses), func(b *testing.B) {
				data := structValidationModule(fields, accesses, 2)
				m, err := DecodeModule(data)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if err := ValidateModule(m); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkValidateStructConstruction(b *testing.B) {
	for _, opcode := range []byte{0, 1} {
		for _, fields := range []int{0, 1, 64} {
			b.Run(fmt.Sprintf("opcode=%d/fields=%d", opcode, fields), func(b *testing.B) {
				m, err := DecodeModule(structValidationModule(fields, 1, opcode))
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					if err := ValidateModule(m); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
