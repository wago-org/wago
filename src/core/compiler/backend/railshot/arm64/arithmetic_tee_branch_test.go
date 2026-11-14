//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/arm64"
)

func arithmeticTeeBranchFixture(t testing.TB, subtract, oldRead, immediate bool) *wasm.Module {
	op := byte(0x6a)
	if subtract {
		op = 0x6b
	}
	b := []byte{1, 2, 0x7f, 0x02, 0x40, 0x03, 0x40,
		0x20, 3, 0x20, 2, 0x4f, 0x0d, 1,
		0x20, 3, 0x41, 1, 0x6a, 0x21, 3}
	if oldRead {
		b = append(b, 0x20, 0)
	}
	b = append(b, 0x20, 0)
	if immediate {
		b = append(b, 0x41, 1)
	} else {
		b = append(b, 0x20, 1)
	}
	b = append(b, op, 0x22, 0, 0x0d, 0)
	if oldRead {
		b = append(b, 0x21, 4)
	}
	b = append(b, 0x0b, 0x0b, 0x20, 0, 0xad, 0x42, 32, 0x86, 0x20, 3, 0xad, 0x84, 0x20, 4, 0xad, 0x85, 0x0b)
	return mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, b)
}

func TestArithmeticTeeBranchWrappingAndOlderReads(t *testing.T) {
	for _, sub := range []bool{false, true} {
		for _, oldRead := range []bool{false, true} {
			for _, immediate := range []bool{false, true} {
				m := arithmeticTeeBranchFixture(t, sub, oldRead, immediate)
				for _, on := range []bool{false, true} {
					for _, compact := range []bool{false, true} {
						opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"arithmetic-tee-branch": on}}
						if diagnosticsEnabled {
							var stats ModuleStats
							opts.Stats = &stats
							cm, err := CompileModuleWith(m, opts)
							if err != nil {
								t.Fatal(err)
							}
							cm.CodeImage.Close()
							if (stats.Funcs[0].Peephole["arithmetic-tee-branch"] > 0) != (on && !oldRead) {
								t.Fatalf("sub%v old%v on%v compact%v hits%v", sub, oldRead, on, compact, stats.Funcs[0].Peephole)
							}
							opts.Stats = nil
						}
						for _, seed := range []uint64{0, 1, 2, 3, 7, 0x7fffffff, 0x80000000, 0xffffffff, 0x8877665500000001} {
							for _, delta := range []uint64{0, 1, 2, 0x7fffffff, 0x80000000, 0xffffffff} {
								for _, limit := range []uint64{0, 1, 3, 9} {
									counter, acc, old := uint32(seed), uint32(0), uint32(0)
									for acc < uint32(limit) {
										previous := counter
										acc++
										if sub {
											step := uint32(delta)
											if immediate {
												step = 1
											}
											counter -= step
										} else {
											step := uint32(delta)
											if immediate {
												step = 1
											}
											counter += step
										}
										if counter == 0 {
											if oldRead {
												old = previous
											}
											break
										}
									}
									want := (uint64(counter)<<32 | uint64(acc)) ^ uint64(old)
									got, err := runArm64WrapperWithOptions(t, m, opts, seed, delta, limit)
									if err != nil || got != want {
										t.Fatalf("sub%v old%v on%v compact%v seed%x delta%x limit%d got%x want%x err%v", sub, oldRead, on, compact, seed, delta, limit, got, want, err)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestArithmeticTeeBranchFlagWindow(t *testing.T) {
	for _, tc := range []struct {
		name string
		emit func(*enc.Asm)
		want bool
	}{
		{"empty", func(a *enc.Asm) {}, true},
		{"spill and constant", func(a *enc.Asm) { a.Store64(X19, SP, 24); a.MovImm64(X10, 27) }, true},
		{"float spill and copy", func(a *enc.Asm) { a.StrF(SP, 24, 8, true); a.FmovReg(9, 8, false) }, true},
		{"counter reload", func(a *enc.Asm) { a.Load64(X19, SP, 24) }, false},
		{"flag overwrite", func(a *enc.Asm) { a.CmpImm32(X10, 0) }, false},
		{"flag read", func(a *enc.Asm) { a.Cset32(X10, condNE) }, false},
		{"unknown call", func(a *enc.Asm) { a.Blr(X9) }, false},
		{"window limit", func(a *enc.Asm) {
			for range 33 {
				a.Nop()
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &enc.Asm{}
			a.SubImm32(X19, X19, 1)
			original := rdWord(a.B, 0)
			tc.emit(a)
			f := fn{a: a}
			got := f.reuseArithmeticZeroFlags(0, X19)
			if got != tc.want {
				t.Fatalf("got%v want%v", got, tc.want)
			}
			want := original
			if tc.want {
				want |= 1 << 29
			}
			if rdWord(a.B, 0) != want {
				t.Fatal("failed proof changed arithmetic")
			}
		})
	}
	for _, word := range []uint32{0xd1000673, 0x91000673, 0x71000673, 0x1100067f, 0x1b137e73, 0xffffffff} {
		if arithmeticZeroWord(word, X19) {
			t.Fatalf("unexpected arithmetic %x", word)
		}
	}
}

func TestArithmeticTeeBranchDeferredTrapOrder(t *testing.T) {
	// Each iteration publishes the old counter before its deferred division.
	b := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40,
		0x20, 2, 0x41, 3, 0x4f, 0x0d, 1,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2,
		0x41, 0, 0x20, 0, 0x36, 0, 0,
		0x20, 0, 0x20, 0, 0x20, 1, 0x6e, 0x6a, 0x22, 0, 0x0d, 0,
		0x0b, 0x0b, 0x41, 4, 0x20, 0, 0x36, 0, 0, 0x41, 0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, b)
	for _, on := range []bool{false, true} {
		for _, seed := range []uint64{0, 1, 7, 0x80000000, 0xffffffff, 0x8877665500000007} {
			for _, divisor := range []uint64{0, 1, 2, 0xffffffff} {
				counter, old := uint32(seed), uint32(0)
				trap := uint32(divisor) == 0
				for i := 0; i < 3; i++ {
					old = counter
					if trap {
						break
					}
					counter += counter / uint32(divisor)
					if counter == 0 {
						break
					}
				}
				got, err := runScalarUnrollMemory(t, m, CompileOptions{Optimizations: map[string]bool{"arithmetic-tee-branch": on}}, seed, divisor)
				if (err != nil) != trap || binary.LittleEndian.Uint32(got) != old || !trap && binary.LittleEndian.Uint32(got[4:]) != counter {
					t.Fatalf("on%v seed%x divisor%x got%x err%v old%x counter%x trap%v", on, seed, divisor, got[:8], err, old, counter, trap)
				}
			}
		}
	}
}
