//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestIfPrefixRematExecution(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := ifPrefixRematEnabled
	defer func() { ifPrefixRematEnabled = saved }()
	for _, affine := range []bool{false, true} {
		for _, manyLocals := range []bool{false, true} {
			for _, merge := range []bool{false, true} {
				for _, compact := range []bool{false, true} {
					for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
						t.Run(fmt.Sprintf("add%v/memory%v/merge%v/compact%v/features%d", affine, manyLocals, merge, compact, features), func(t *testing.T) {
							body := []byte{0}
							if manyLocals {
								body = []byte{1, 65, 0x7f}
							}
							body = append(body, 0x20, 0)
							if affine {
								body = append(body, 0x41, 17, 0x6a)
							}
							// Other locals may change. The prefix's input may not.
							body = append(body, 0x20, 1, 0x04, 0x7f,
								0x20, 2, 0x21, 3, 0x20, 3, 0x41, 7, 0x73,
								0x05, 0x20, 2, 0x41, 11, 0x6a, 0x22, 3,
								0x0b, 0x6a, 0x20, 0, 0x73, 0x0b)
							m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
							for _, enabled := range []bool{false, true} {
								ifPrefixRematEnabled = enabled
								var stats ModuleStats
								cm, err := CompileModuleWith(m, CompileOptions{Stats: &stats, Profile: profileEnabled, SourceMaps: true, CompactNative: compact, AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"reg-merge": merge}})
								if err != nil {
									t.Fatal(err)
								}
								if cm.CodeImage != nil {
									defer cm.CodeImage.Close()
								}
								if (stats.Funcs[0].Peephole["if-prefix-remat"] == 1) != enabled {
									t.Fatal("admission", enabled, stats.Funcs[0].Peephole)
								}
								for _, x := range []uint64{0, 1, 0xffffffff, 0xcafe000080000000} {
									for _, c := range []uint64{0, 1, 0xffffffff, 0x100000000} {
										v := uint32(x) * 13
										arm := v + 11
										if uint32(c) != 0 {
											arm = v ^ 7
										}
										prefix := uint32(x)
										if affine {
											prefix += 17
										}
										want := (prefix + arm) ^ uint32(x)
										if got := runCompiledAmd64u(t, cm, x, c, uint64(v), 42); uint32(got) != want {
											t.Fatalf("on=%v x=%x c=%x got=%x want=%x", enabled, x, c, got, want)
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

func TestIfPrefixRematProof(t *testing.T) {
	f := &fn{classifier: wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)}
	for _, tc := range []struct {
		name string
		code []byte
		want bool
	}{
		{"read", []byte{0x20, 0, 0x05, 0x20, 1, 0x0b}, true},
		{"other-write", []byte{0x41, 1, 0x21, 1, 0x41, 2, 0x05, 0x41, 3, 0x0b}, true},
		{"write", []byte{0x41, 1, 0x21, 0, 0x41, 2, 0x05, 0x41, 3, 0x0b}, false},
		{"tee", []byte{0x41, 1, 0x22, 0, 0x05, 0x41, 3, 0x0b}, false},
		{"branch", []byte{0x41, 1, 0x0c, 0, 0x05, 0x41, 3, 0x0b}, false},
		{"nested", []byte{0x02, 0x40, 0x0b, 0x41, 1, 0x05, 0x41, 3, 0x0b}, false},
		{"call", []byte{0x10, 0, 0x05, 0x41, 3, 0x0b}, false},
		{"missing-else", []byte{0x41, 1, 0x0b}, false},
		{"truncated", []byte{0x41, 0x80}, false},
	} {
		r := wasm.NewReader(tc.code)
		if got := f.ifPrefixUnchanged(r, 0); got != tc.want || r.Offset() != 0 {
			t.Fatal(tc.name, got, r.Offset())
		}
	}
	long := make([]byte, 60)
	for i := range long {
		long[i] = 1
	}
	long = append(long, 0x05, 0x0b)
	if f.ifPrefixUnchanged(wasm.NewReader(long), 0) {
		t.Fatal("unbounded scan")
	}
	local := &elem{st: storage{kind: stLocalRef, typ: mtI32, idx: 1}}
	for _, kind := range []storageKind{stReg, stSlot, stMemRef, stGlobalRef} {
		bad := &elem{st: storage{kind: kind, typ: mtI32}}
		if ifPrefixLocal(bad) != nil {
			t.Fatal("non-rematerializable value", kind)
		}
	}
	if ifPrefixLocal(local) != local {
		t.Fatal("local rejected")
	}
}

func TestIfPrefixRematTrapAndEffects(t *testing.T) {
	saved := ifPrefixRematEnabled
	defer func() { ifPrefixRematEnabled = saved }()
	body := []byte{0, 0x20, 0, 0x41, 17, 0x6a, 0x20, 1, 0x04, 0x7f,
		0x41, 0, 0x41, 29, 0x36, 2, 0, 0x20, 0, 0x41, 0, 0x6e,
		0x05, 0x41, 0, 0x41, 19, 0x36, 2, 0, 0x41, 7,
		0x0b, 0x6a, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, on := range []bool{false, true} {
		ifPrefixRematEnabled = on
		for _, cond := range []uint64{0, 1} {
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 42, cond)
			if cond == 1 {
				if err == nil || binary.LittleEndian.Uint32(mem) != 29 {
					t.Fatal("lost trap or preceding store", on, got, err)
				}
			} else if err != nil || uint32(got) != 66 || binary.LittleEndian.Uint32(mem) != 19 {
				t.Fatal("executed untaken arm", on, got, err)
			}
		}
	}
}

func TestIfPrefixRematRejectsWrittenInput(t *testing.T) {
	requireCompilerDiagnostics(t)
	saved := ifPrefixRematEnabled
	ifPrefixRematEnabled = true
	defer func() { ifPrefixRematEnabled = saved }()
	// The get-time prefix must survive an arm's write to the same Wasm local.
	body := []byte{0, 0x20, 0, 0x41, 17, 0x6a, 0x20, 1, 0x04, 0x7f,
		0x41, 9, 0x21, 0, 0x41, 3, 0x05, 0x41, 5, 0x0b, 0x6a, 0x20, 0, 0x6a, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, cond := range []uint64{0, 1} {
		var stats ModuleStats
		got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: &stats}, nil, 42, cond)
		want := uint32(42 + 17 + 5 + 42)
		if cond != 0 {
			want = 42 + 17 + 3 + 9
		}
		if err != nil || uint32(got) != want || stats.Funcs[0].Peephole["if-prefix-remat"] != 0 {
			t.Fatal("changed input admitted or get-time value lost", cond, got, err, stats.Funcs[0].Peephole)
		}
	}
}
