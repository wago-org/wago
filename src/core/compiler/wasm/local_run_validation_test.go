package wasm

import (
	"errors"
	"fmt"
	"testing"
)

type localLookupValidationFunc struct {
	params []ValType
	runs   []LocalRun
	ops    []byte
}

func localLookupValidationModule(funcs ...localLookupValidationFunc) []byte {
	types, functions, code := u32(uint32(len(funcs))), u32(uint32(len(funcs))), u32(uint32(len(funcs)))
	for i, fn := range funcs {
		types = append(types, 0x60)
		types = append(types, u32(uint32(len(fn.params)))...)
		for _, param := range fn.params {
			types = append(types, MustEncodeValType(param))
		}
		types = append(types, 0) // no results
		functions = append(functions, u32(uint32(i))...)
		body := localRunsBody(fn.runs)
		body = append(body[:len(body)-1], fn.ops...)
		body = append(body, 0x0b)
		code = append(code, u32(uint32(len(body)))...)
		code = append(code, body...)
	}
	return module(section(secType, types...), section(secFunction, functions...), section(secCode, code...))
}

func checkLocalLookupValidation(t *testing.T, source []byte, workers int, want ValidationErrorCode) {
	t.Helper()
	for _, path := range []string{"AST", "byte-backed"} {
		t.Run(path, func(t *testing.T) {
			var err error
			if path == "AST" {
				var m *Module
				m, err = decodeModuleASTForTest(source)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				err = ValidateModuleWithWorkers(m, workers)
			} else {
				var dm *DecodedByteBackedModule
				dm, err = DecodeModuleByteBacked(source)
				if err != nil {
					t.Fatalf("decode: %v", err)
				}
				err = ValidateDecodedByteBackedModuleWithWorkers(dm, workers)
			}
			if want < 0 {
				if err != nil {
					t.Fatalf("validate: %v", err)
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Code != want {
				t.Fatalf("validate error=%v, want code %v", err, want)
			}
		})
	}
}

func localLookupOp(ops []byte, opcode byte, index uint32) []byte {
	return append(append(ops, opcode), u32(index)...)
}

func localLookupZeroAndAdd(t *testing.T, typ ValType) ([]byte, byte) {
	t.Helper()
	switch typ {
	case I32:
		return []byte{0x41, 0}, 0x6a
	case I64:
		return []byte{0x42, 0}, 0x7c
	case F32:
		return []byte{0x43, 0, 0, 0, 0}, 0x92
	case F64:
		return []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0}, 0xa0
	default:
		t.Fatalf("unsupported test local type %v", typ)
		return nil, 0
	}
}

func localLookupWarmup(index uint32) []byte {
	var ops []byte
	for i := 0; i < 16; i++ {
		ops = localLookupOp(ops, 0x20, index)
		ops = append(ops, 0x1a)
	}
	return ops
}

func localLookupValidOps(t *testing.T, params []ValType, runs []LocalRun) []byte {
	t.Helper()
	count, _ := LocalCount(params, runs)
	var ops []byte
	if count != 0 {
		ops = localLookupWarmup(uint32(count - 1))
	}
	for idx := uint32(0); uint64(idx) < count; idx++ {
		typ, ok := LocalType(params, runs, idx)
		if !ok {
			t.Fatalf("missing fixture local %d", idx)
		}
		zero, add := localLookupZeroAndAdd(t, typ)
		ops = localLookupOp(ops, 0x20, idx) // local.get
		ops = append(ops, zero...)
		ops = append(ops, add)              // forces local.get's expected numeric type
		ops = localLookupOp(ops, 0x22, idx) // local.tee
		ops = localLookupOp(ops, 0x21, idx) // local.set consumes tee's result
	}
	return ops
}

func TestValidateLazyLocalRunsMixedTypes(t *testing.T) {
	runs := []LocalRun{
		{Type: F64},
		{Count: 2, Type: I32},
		{Type: I64},
		{Count: 1, Type: F32},
		{Count: 3, Type: I64},
		{Type: F32},
		{Count: 2, Type: F64},
		{Type: I32},
		{Type: F64},
	}
	for _, params := range [][]ValType{nil, {F32, I32, F64, I64}} {
		t.Run(fmt.Sprintf("params=%d", len(params)), func(t *testing.T) {
			fn := localLookupValidationFunc{params: params, runs: runs, ops: localLookupValidOps(t, params, runs)}
			checkLocalLookupValidation(t, localLookupValidationModule(fn), 1, -1)
		})
	}
	// An all-zero run list has no declared locals despite its many entries.
	params := []ValType{I64, F64}
	zeroRuns := make([]LocalRun, 64)
	for i := range zeroRuns {
		zeroRuns[i].Type = []ValType{I32, F32, I64, F64}[i%4]
	}
	t.Run("zero-count-runs", func(t *testing.T) {
		fn := localLookupValidationFunc{params: params, runs: zeroRuns, ops: localLookupValidOps(t, params, zeroRuns)}
		checkLocalLookupValidation(t, localLookupValidationModule(fn), 1, -1)
	})
}

func TestValidateLazyLocalRunsTypeMismatch(t *testing.T) {
	params := []ValType{F64, I64}
	runs := make([]LocalRun, 16)
	for i := range runs {
		runs[i] = LocalRun{Count: uint32(i % 3), Type: []ValType{I32, I64, F32, F64}[i%4]}
	}
	count, _ := LocalCount(params, runs)
	for _, warm := range []bool{false, true} {
		for _, idx := range []uint32{0, uint32(len(params)), uint32(count - 1)} {
			for _, opcode := range []byte{0x20, 0x21, 0x22} {
				t.Run(fmt.Sprintf("warm=%t/local=%d/op=%x", warm, idx, opcode), func(t *testing.T) {
					typ, _ := LocalType(params, runs, idx)
					wrong := I32
					if typ == I32 {
						wrong = F64
					}
					zero, add := localLookupZeroAndAdd(t, wrong)
					var ops []byte
					if warm {
						ops = localLookupWarmup(uint32(count - 1))
					}
					if opcode == 0x20 {
						ops = localLookupOp(ops, opcode, idx)
						ops = append(ops, zero...)
						ops = append(ops, add, 0x1a)
					} else {
						ops = append(ops, zero...)
						ops = localLookupOp(ops, opcode, idx)
						if opcode == 0x22 {
							ops = append(ops, 0x1a)
						}
					}
					fn := localLookupValidationFunc{params: params, runs: runs, ops: ops}
					checkLocalLookupValidation(t, localLookupValidationModule(fn), 1, ErrTypeMismatch)
				})
			}
		}
	}
}

func TestValidateLazyLocalRunsBounds(t *testing.T) {
	params := []ValType{I64, F64}
	for _, allZero := range []bool{false, true} {
		runs := make([]LocalRun, 16)
		for i := range runs {
			runs[i].Type = I32
			if !allZero && i%3 != 0 {
				runs[i].Count = 1
			}
		}
		count, _ := LocalCount(params, runs)
		for _, warm := range []bool{false, true} {
			for _, idx := range []uint32{uint32(count), uint32(count + 1), ^uint32(0)} {
				for _, opcode := range []byte{0x20, 0x21, 0x22} {
					t.Run(fmt.Sprintf("zero=%t/warm=%t/local=%d/op=%x", allZero, warm, idx, opcode), func(t *testing.T) {
						var ops []byte
						if warm {
							ops = localLookupWarmup(uint32(count - 1))
						}
						if opcode != 0x20 {
							ops = append(ops, 0x41, 0)
						}
						ops = localLookupOp(ops, opcode, idx)
						if opcode != 0x21 {
							ops = append(ops, 0x1a)
						}
						fn := localLookupValidationFunc{params: params, runs: runs, ops: ops}
						checkLocalLookupValidation(t, localLookupValidationModule(fn), 1, ErrUnknownLocal)
					})
				}
			}
		}
	}
}

func TestValidateLazyLocalRunsFunctionReuse(t *testing.T) {
	var funcs []localLookupValidationFunc
	for i, n := range []int{2048, 2048, 2, 64, 0, 32, 3, 2048, 1} {
		params := []ValType{F32, I64}[:i%3]
		runs := make([]LocalRun, n)
		for j := range runs {
			runs[j] = LocalRun{Count: uint32((i + j) % 3), Type: []ValType{I32, I64, F32, F64}[(i+j)%4]}
		}
		funcs = append(funcs, localLookupValidationFunc{params: params, runs: runs, ops: localLookupValidOps(t, params, runs)})
	}
	source := localLookupValidationModule(funcs...)
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			checkLocalLookupValidation(t, source, workers, -1)
		})
	}
}
