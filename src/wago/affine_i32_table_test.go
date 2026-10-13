//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestAffineI32TablePublicExecution(t *testing.T) {
	for _, scale := range []uint32{0, 1, 2, 0xffffffff, 0x80000001} {
		for _, n := range []int{1, 2, 3, 16} {
			for _, exported := range []bool{false, true} {
				for _, inline := range []bool{false, true} {
					for _, abi := range []bool{false, true} {
						t.Run(fmt.Sprintf("scale%x/n%d/export%t/inline%t/abi%t", scale, n, exported, inline, abi), func(t *testing.T) {
							biases := make([]uint32, n)
							for i := range biases {
								biases[i] = 0xfffffff1 + uint32(i)*0x80000007
							}
							c, err := NewRuntimeConfig().WithOptimization("inline", inline).WithOptimization("reg-abi", abi).Compile(wasmtest.AffineI32Table(scale, biases, uint32(n), exported, false))
							if err != nil {
								t.Fatal(err)
							}
							defer c.Close()
							loaded := publicArtifactRoundTrip(t, c)
							defer loaded.Close()
							for _, compiled := range []*Compiled{c, loaded} {
								in, err := Instantiate(compiled)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								for _, value := range []uint32{0, 1, 0x80000000, 0xffffffff} {
									for idx, bias := range biases {
										args := []uint64{uint64(value) | 0xfedcba9800000000, uint64(idx) | 0xabcdef1200000000}
										want := uint64(value*scale + bias)
										for _, name := range []string{"call", "prefix"} {
											got, err := in.Invoke(name, args...)
											expected := want
											if name == "prefix" {
												expected += 0x1122334455667788
											}
											if err != nil || len(got) != 1 || got[0] != expected {
												t.Fatalf("%s idx%d value%x => %v %v, want%x", name, idx, value, got, err, expected)
											}
										}
									}
									for _, count := range []uint32{1, 17, 64} {
										var want uint64
										for i := uint32(0); i < count; i++ {
											want += uint64((value+i)*scale + biases[int(i)%n])
										}
										got, err := in.Invoke("loop", uint64(value), uint64(count))
										if err != nil || len(got) != 1 || got[0] != want {
											t.Fatalf("loop count%d =>%v %v want%x", count, got, err, want)
										}
									}
								}
								for _, name := range []string{"call", "prefix", "wrong"} {
									for _, idx := range []uint32{0, uint32(n), 0xffffffff} {
										if name != "wrong" && idx == 0 {
											continue
										}
										_, err := in.Invoke(name, 7, uint64(idx))
										want := TrapIndirectOutOfBounds
										if idx == 0 {
											want = TrapIndirectWrongSig
										}
										var trap *TrapError
										if !errors.As(err, &trap) || trap.Code != want {
											t.Fatalf("%s idx%d =>%v want trap%d", name, idx, err, want)
										}
									}
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestAffineI32TableArgumentTrapOrder(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, scale := range []uint32{0, 3} {
			t.Run(fmt.Sprintf("inline%t/scale%d", inline, scale), func(t *testing.T) {
				c, err := NewRuntimeConfig().WithOptimization("inline", inline).Compile(wasmtest.AffineI32Table(scale, []uint32{5, 9}, 2, false, true))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				loaded := publicArtifactRoundTrip(t, c)
				defer loaded.Close()
				for _, compiled := range []*Compiled{c, loaded} {
					in, err := Instantiate(compiled)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					binary.LittleEndian.PutUint32(in.Memory().UnsafeBytes()[65532:], 0xabcdef01)
					got, err := in.Invoke("call", 65532, 1)
					want := uint64(uint32(0xabcdef01)*scale + 9)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("valid load: %v %v want%x", got, err, want)
					}
					for _, args := range [][2]uint64{{65533, 2}, {65536, 0}, {0xffffffff, 0xffffffff}} {
						_, err := in.Invoke("call", args[0], args[1])
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
							t.Fatalf("args%v =>%v; argument load must trap first", args, err)
						}
					}
				}
			})
		}
	}
}
