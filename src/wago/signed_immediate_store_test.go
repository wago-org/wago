//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

var signedImmediateValues = []int64{0, 1, -1, math.MinInt32, math.MaxInt32, math.MinInt32 - 1, math.MaxInt32 + 1, 0xffffffff, 0x1234567887654321, math.MinInt64, math.MaxInt64}

func compileSignedImmediateStore(t testing.TB, module []byte, mode BoundsCheckMode) (*Compiled, *Instance) {
	t.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(mode), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { compiled.Close() })
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })
	return compiled, instance
}

func TestSignedImmediateStoreBytesAndTraps(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		for _, value := range signedImmediateValues {
			for _, size := range []int{1, 2, 4, 8} {
				for _, offset := range []uint32{0, 7, 128, math.MaxInt32, math.MaxUint32 - 7, math.MaxUint32} {
					t.Run(fmt.Sprintf("mode=%d/value=%x/size=%d/offset=%d", mode, uint64(value), size, offset), func(t *testing.T) {
						_, instance := compileSignedImmediateStore(t, wasmtest.SignedImmediateStore(value, size, false, false, false, offset), mode)
						mem := instance.Memory().UnsafeBytes()
						addresses := []uint64{0, 1, 63, 64, math.MaxUint32}
						if uint64(offset) <= uint64(len(mem)-size) {
							// Check every partial-access length, including the point at
							// which a split i64 store's first dword would succeed.
							for accessible := 0; accessible <= size; accessible++ {
								addresses = append(addresses, uint64(len(mem)-accessible)-uint64(offset))
							}
						}
						for _, addr := range addresses {
							for i := range mem {
								mem[i] = byte(i*17 + 91)
							}
							want := append([]byte(nil), mem...)
							start := addr + uint64(offset)
							valid := start+uint64(size) <= uint64(len(mem))
							_, err := instance.Invoke("run", addr)
							if valid {
								if err != nil {
									t.Fatalf("addr=%d: %v", addr, err)
								}
								var bits [8]byte
								binary.LittleEndian.PutUint64(bits[:], uint64(value))
								copy(want[start:start+uint64(size)], bits[:size])
							} else {
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
									t.Fatalf("addr=%d trap=%v", addr, err)
								}
							}
							if !bytes.Equal(mem, want) {
								t.Fatalf("addr=%d wrote incorrect bytes or changed memory before trapping", addr)
							}
						}
					})
				}
			}
		}
	}
}

// Independently reject an incorrect sign-extension substitution before any
// candidate native code is executed. Checking only the low dword would miss it.
func TestSignedImmediateStoreSignExtensionControl(t *testing.T) {
	for _, value := range signedImmediateValues {
		var full, proposed [8]byte
		binary.LittleEndian.PutUint64(full[:], uint64(value))
		binary.LittleEndian.PutUint64(proposed[:], uint64(int64(int32(value))))
		accepted := bytes.Equal(full[:], proposed[:])
		want := value >= math.MinInt32 && value <= math.MaxInt32
		if accepted != want {
			t.Fatalf("value=%x accepted=%v want=%v", uint64(value), accepted, want)
		}
	}
	for _, wrong := range []int64{0x80000000, 0xffffffff, math.MinInt32 - 1, 0x1234567887654321} {
		if int64(int32(wrong)) == wrong {
			t.Fatalf("wrong-sign-extension control unexpectedly accepted: %x", uint64(wrong))
		}
	}
}

var signedImmediateBenchCases = []struct {
	name             string
	value            int64
	size             int
	pressure, addr64 bool
	base             uint64
}{
	{"zero", 0, 8, false, false, 0}, {"ones", -1, 8, false, false, 0},
	{"min32", math.MinInt32, 8, false, false, 0}, {"max32", math.MaxInt32, 8, false, false, 0},
	{"near-positive", math.MaxInt32 + 1, 8, false, false, 0}, {"near-negative", math.MinInt32 - 1, 8, false, false, 0},
	{"mixed-halves", 0x1234567887654321, 8, false, false, 0},
	{"unaligned", -1, 8, false, false, 1}, {"cacheline-crossing", -1, 8, false, false, 63},
	{"pressure", -1, 8, true, false, 0}, {"pressure-near-miss", 0x80000000, 8, true, false, 0},
	{"store8-control", -1, 1, false, false, 0}, {"store16-control", -1, 2, false, false, 0}, {"store32-control", -1, 4, false, false, 0},
	{"memory64-control", -1, 8, false, true, 0},
}

func BenchmarkSignedImmediateStore(b *testing.B) {
	benchmarkSignedImmediateStore(b, BoundsChecksExplicit)
}

func BenchmarkGuardSignedImmediateStore(b *testing.B) {
	if !GuardPageSupported() {
		b.Skip("guard-page support requires wago_guardpage")
	}
	benchmarkSignedImmediateStore(b, BoundsChecksSignalsBased)
}

func benchmarkSignedImmediateStore(b *testing.B, mode BoundsCheckMode) {
	for _, tc := range signedImmediateBenchCases {
		b.Run(tc.name, func(b *testing.B) {
			compiled, instance := compileSignedImmediateStore(b, wasmtest.SignedImmediateStore(tc.value, tc.size, true, tc.pressure, tc.addr64, 0), mode)
			const iterations = 4096
			args := []uint64{iterations, tc.base, 1}
			got, err := instance.Invoke("run", args...)
			wantSum := uint64(0)
			if tc.pressure {
				wantSum = 7 * iterations
			}
			if err != nil || len(got) != 1 || got[0] != wantSum {
				b.Fatalf("warmup=%v err=%v want=%d", got, err, wantSum)
			}
			check := func() {
				mem := instance.Memory().UnsafeBytes()
				var bits [8]byte
				binary.LittleEndian.PutUint64(bits[:], uint64(tc.value))
				for group := 0; group < 128; group++ {
					for j := 0; j < 4; j++ {
						start := int(tc.base) + group*32 + j*8
						if !bytes.Equal(mem[start:start+tc.size], bits[:tc.size]) {
							b.Fatalf("memory mismatch at %d", start)
						}
					}
				}
			}
			check()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, err = instance.Invoke("run", args...)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if got[0] != wantSum {
				b.Fatalf("sum=%d want=%d", got[0], wantSum)
			}
			check()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*iterations*4), "ns/store")
			b.ReportMetric(float64(compiled.CodeSize()), "code-B")
		})
	}
}

func BenchmarkCompileSignedImmediateStore(b *testing.B) {
	for _, tc := range signedImmediateBenchCases {
		b.Run(tc.name, func(b *testing.B) {
			module := wasmtest.SignedImmediateStore(tc.value, tc.size, true, tc.pressure, tc.addr64, 0)
			cfg := NewRuntimeConfig()
			size := 0
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cm, err := Compile(cfg, module)
				if err != nil {
					b.Fatal(err)
				}
				size = cm.CodeSize()
				cm.Close()
			}
			b.ReportMetric(float64(size), "code-B")
		})
	}
}

func TestSignedImmediateStoreAddressEffectsAndReuse(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		for _, value := range []int64{-1, math.MinInt32, math.MaxInt32 + 1} {
			t.Run(fmt.Sprintf("mode=%d/value=%x", mode, uint64(value)), func(t *testing.T) {
				body := []byte{1, 1, 0x7e, 0x42}
				body = append(body, wasmtest.SLEB64(value)...)
				body = append(body,
					0x21, 1, // retain the constant local for the later result
					0x41, 16, 0x41, 0xd5, 0x00, 0x3a, 0, 0, // observable pre-store marker
					0x20, 0, 0x10, 1, 0x20, 1, 0x37, 0, 0, // effectful address producer; constant store
					0x41, 17, 0x41, 42, 0x3a, 0, 0, // observable post-store marker
					0x20, 1, 0x0b)
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(
						wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I64}),
						wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
						wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
					wasmtest.Section(5, []byte{1, 0, 1}),
					wasmtest.Section(6, []byte{1, 0x7f, 1, 0x41, 0, 0x0b}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("count", 0, 2), wasmtest.ExportEntry("memory", 2, 0))),
					wasmtest.Section(10, wasmtest.Vec(
						append(wasmtest.ULEB(uint32(len(body))), body...),
						wasmtest.Code([]byte{0x23, 0, 0x41, 1, 0x6a, 0x24, 0, 0x20, 0, 0x0b}),
						wasmtest.Code([]byte{0x23, 0, 0x0b}))),
				)
				_, instance := compileSignedImmediateStore(t, module, mode)
				for step, addr := range []uint64{63, 65529} {
					mem := instance.Memory().UnsafeBytes()
					for i := range mem {
						mem[i] = 0xa7
					}
					got, err := instance.Invoke("run", addr)
					if mem[16] != 0x55 {
						t.Fatal("pre-store effect missing")
					}
					if addr == 63 {
						if err != nil || len(got) != 1 || got[0] != uint64(value) {
							t.Fatalf("reused result=%x err=%v", got, err)
						}
						if binary.LittleEndian.Uint64(mem[63:]) != uint64(value) || mem[17] != 42 {
							t.Fatal("valid store or post-store effect differs")
						}
					} else {
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
							t.Fatalf("trap=%v", err)
						}
						if mem[17] != 0xa7 || !bytes.Equal(mem[65529:], bytes.Repeat([]byte{0xa7}, 7)) {
							t.Fatal("effect moved before trapping store")
						}
					}
					count, err := instance.Invoke("count")
					if err != nil || len(count) != 1 || count[0] != uint64(step+1) {
						t.Fatalf("address evaluation count=%v err=%v", count, err)
					}
				}
			})
		}
	}
}

func TestSignedImmediateStorePressureTraps(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		for _, value := range []int64{-1, math.MinInt32, math.MaxInt32 + 1, 0x1234567887654321, math.MinInt64} {
			t.Run(fmt.Sprintf("mode=%d/value=%x", mode, uint64(value)), func(t *testing.T) {
				_, instance := compileSignedImmediateStore(t, wasmtest.SignedImmediateStore(value, 8, true, true, false, 0), mode)
				mem := instance.Memory().UnsafeBytes()
				for _, addr := range []uint64{63, 65528, 65529, 65532, 65535, math.MaxUint32} {
					for i := range mem {
						mem[i] = byte(i*17 + 91)
					}
					want := append([]byte(nil), mem...)
					var bits [8]byte
					binary.LittleEndian.PutUint64(bits[:], uint64(value))
					trapping := false
					for j := uint64(0); j < 4; j++ {
						start := addr + j*8
						if start+8 > uint64(len(mem)) {
							trapping = true
							break
						}
						copy(want[start:start+8], bits[:])
					}
					got, err := instance.Invoke("run", 1, addr, 7)
					if trapping {
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
							t.Fatalf("addr=%d trap=%v", addr, err)
						}
					} else if err != nil || len(got) != 1 || got[0] != 49 {
						t.Fatalf("addr=%d checksum=%v err=%v", addr, got, err)
					}
					if !bytes.Equal(mem, want) {
						t.Fatalf("addr=%d changed bytes beyond completed guest stores", addr)
					}
				}
			})
		}
	}
}
