package wasm

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

type localRunLookupFunction struct {
	runs  int
	reads []uint32
}

type localRunLookupWorkload struct {
	name  string
	funcs []localRunLookupFunction
}

func localRunLookupReads(runs, count int, pattern string) []uint32 {
	reads := make([]uint32, count)
	switch pattern {
	case "late":
		for i := range reads {
			reads[i] = uint32(runs - 1 - i%runs)
		}
	case "early":
		// The zero-filled slice repeatedly accesses the first local run.
	case "random":
		// Cycle a fixed permutation so repeated runs and both representations
		// validate exactly the same accesses without timing random generation.
		order := rand.New(rand.NewSource(1)).Perm(runs)
		for i := range reads {
			reads[i] = uint32(order[i%len(order)])
		}
	default:
		panic("unknown local lookup pattern")
	}
	return reads
}

func localRunLookupWorkloads() []localRunLookupWorkload {
	var workloads []localRunLookupWorkload
	add := func(runs, reads int, pattern string) {
		name := fmt.Sprintf("runs=%d/%s_%d", runs, pattern, reads)
		if reads == 0 {
			name = fmt.Sprintf("runs=%d/no_reads", runs)
		}
		workloads = append(workloads, localRunLookupWorkload{
			name: name,
			funcs: []localRunLookupFunction{{
				runs: runs, reads: localRunLookupReads(runs, reads, pattern),
			}},
		})
	}
	add(1, 1, "late")
	add(1, 256, "random")
	add(3, 256, "random")
	add(8, 1, "late")
	add(8, 256, "random")
	for _, runs := range []int{64, 1024} {
		add(runs, 0, "early")
		add(runs, 1, "late")
		add(runs, 4, "late")
		add(runs, 256, "early")
		add(runs, max(256, runs), "random")
	}
	add(16384, 1, "late")

	// One complete validation reuses its worker between these functions. These
	// shapes expose retained-index reuse and shrinking after a large function.
	large := localRunLookupFunction{runs: 4096, reads: localRunLookupReads(4096, 1024, "random")}
	workloads = append(workloads, localRunLookupWorkload{
		name:  "reuse/large_4096_random_1024_x4",
		funcs: []localRunLookupFunction{large, large, large, large},
	})
	mixed := []localRunLookupFunction{large}
	for i := 0; i < 16; i++ {
		mixed = append(mixed, localRunLookupFunction{runs: 8})
	}
	return append(workloads, localRunLookupWorkload{
		name:  "reuse/large_4096_random_1024_then_small_8_no_reads_x16",
		funcs: mixed,
	})
}

func localRunLookupExpr(reads []uint32) []byte {
	var expr []byte
	for _, index := range reads {
		expr = append(expr, 0x20) // local.get
		expr = append(expr, u32(index)...)
		expr = append(expr, 0x1a) // drop
	}
	return append(expr, 0x0b) // end
}

func localRunLookupModule(workload localRunLookupWorkload) []byte {
	funcs := u32(uint32(len(workload.funcs)))
	code := u32(uint32(len(workload.funcs)))
	for _, fn := range workload.funcs {
		funcs = append(funcs, 0) // All functions have the same () -> () type.
		body := u32(uint32(fn.runs))
		for i := 0; i < fn.runs; i++ {
			body = append(body, 1, 0x7f) // One i32 per declared run.
		}
		body = append(body, localRunLookupExpr(fn.reads)...)
		code = append(code, u32(uint32(len(body)))...)
		code = append(code, body...)
	}
	return module(
		section(secType, 1, 0x60, 0, 0),
		section(secFunction, funcs...),
		section(secCode, code...),
	)
}

func BenchmarkValidateLocalRunLookup(b *testing.B) {
	for _, workload := range localRunLookupWorkloads() {
		b.Run(workload.name, func(b *testing.B) {
			source := localRunLookupModule(workload)
			b.Run("AST", func(b *testing.B) {
				m, err := decodeModuleASTForTest(source)
				if err != nil {
					b.Fatal(err)
				}
				if err := ValidateModule(m); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				// Each public call creates fresh validation worker state, so
				// index construction is measured rather than warmed away.
				for i := 0; i < b.N; i++ {
					if err := ValidateModule(m); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("ByteBacked", func(b *testing.B) {
				m, err := DecodeModuleByteBacked(source)
				if err != nil {
					b.Fatal(err)
				}
				if err := ValidateDecodedByteBackedModule(m); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := ValidateDecodedByteBackedModule(m); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func TestValidateLocalRunBenchmarkFixtures(t *testing.T) {
	for _, workload := range localRunLookupWorkloads() {
		t.Run(workload.name, func(t *testing.T) {
			source := localRunLookupModule(workload)
			ast, err := decodeModuleASTForTest(source)
			if err != nil {
				t.Fatal(err)
			}
			direct, err := DecodeModuleByteBacked(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range []*Module{ast, direct.Module} {
				if len(m.Code) != len(workload.funcs) {
					t.Fatalf("decoded %d functions, want %d", len(m.Code), len(workload.funcs))
				}
				for i, fn := range m.Code {
					if len(fn.Locals.Runs) != workload.funcs[i].runs {
						t.Fatalf("function %d: decoded %d runs, want %d", i, len(fn.Locals.Runs), workload.funcs[i].runs)
					}
					for _, run := range fn.Locals.Runs {
						if run.Count != 1 || run.Type != I32 {
							t.Fatalf("function %d: unexpected local run %+v", i, run)
						}
					}
				}
			}
			for i, fn := range workload.funcs {
				if len(ast.Code[i].BodyBytes) != 0 || len(ast.Code[i].Body.BodyBytes) != 0 {
					t.Fatalf("function %d: AST fixture contains byte-backed instructions", i)
				}
				instrs := ast.Code[i].Body.Instrs
				if len(instrs) != 2*len(fn.reads) {
					t.Fatalf("function %d: decoded %d instructions, want %d", i, len(instrs), 2*len(fn.reads))
				}
				for j, index := range fn.reads {
					if instrs[2*j].Kind != InstrLocalGet || instrs[2*j].Index != index || instrs[2*j+1].Kind != InstrDrop {
						t.Fatalf("function %d: read %d differs from local.get %d; drop", i, j, index)
					}
				}
				if len(direct.Module.Code[i].Body.Instrs) != 0 || !bytes.Equal(direct.Module.Code[i].BodyBytes, localRunLookupExpr(fn.reads)) {
					t.Fatalf("function %d: byte-backed fixture has the wrong representation or expression", i)
				}
			}
			for repeat := 0; repeat < 2; repeat++ {
				if err := ValidateModule(ast); err != nil {
					t.Fatalf("AST validation %d: %v", repeat, err)
				}
				if err := ValidateDecodedByteBackedModule(direct); err != nil {
					t.Fatalf("byte-backed validation %d: %v", repeat, err)
				}
			}
		})
	}
}
