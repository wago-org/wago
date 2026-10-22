//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	nativegc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const rv64PublicGCExternHex = "0061736d0100000001a180808000086000005f005e780060016f0060016f016e60016e016f60017f016f60017f016e03878080800006000304050607048480808000016e000a068f80808000026f00d06efb1b0b6e00d06ffb1a0b07c5808080000504696e697400010b696e7465726e616c697a6500020b65787465726e616c697a6500030d65787465726e616c697a652d6900040e65787465726e616c697a652d6969000509858080800001030001000ae780808000068280808000000ba880808000004100d06e260041014107fb1c26004102fb0101260041034100fb0702260041042000fb1a26000b8680808000002000fb1a0b8680808000002000fb1b0b88808080000020002500fb1b0b8a808080000020002500fb1bfb1a0b"

func rv64PublicGCExternBytes(t testing.TB) []byte {
	t.Helper()
	b, err := hex.DecodeString(rv64PublicGCExternHex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func rv64PublicGCExternCompile(t *testing.T, artifact bool) *Compiled {
	t.Helper()
	dir, mode := os.Getenv("WAGO_RV64_GC_EXTERN_ARTIFACT_DIR"), os.Getenv("WAGO_RV64_GC_EXTERN_ARTIFACT_MODE")
	path := filepath.Join(dir, "gc-extern.wago")
	if mode == "load" {
		if !artifact {
			t.Skip("compiler-free runner selected artifact case")
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadTrustedArtifact(blob)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = loaded.Close() })
		return loaded
	}
	original, err := rv64I31Config().Compile(rv64PublicGCExternBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Close() })
	if !artifact && mode != "prepare" {
		return original
	}
	blob, err := original.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if mode == "prepare" {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, blob, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !artifact {
		return original
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func TestRV64PublicGCExternExecutionAndArtifact(t *testing.T) {
	data := rv64PublicGCExternBytes(t)
	if len(data) != 286 || sha256.Sum256(data) != [sha256.Size]byte{0x5a, 0xd9, 0x21, 0xeb, 0xe5, 0x11, 0xca, 0x9e, 0x23, 0xc1, 0x37, 0xae, 0xf6, 0x88, 0x31, 0x13, 0x68, 0x48, 0x96, 0xf1, 0x5b, 0x8a, 0x97, 0x26, 0xd5, 0xd7, 0x75, 0x24, 0xd5, 0x62, 0xf8, 0x23} {
		t.Fatal("gc/extern source identity changed")
	}
	for _, artifact := range []bool{false, true} {
		t.Run(map[bool]string{false: "source", true: "artifact"}[artifact], func(t *testing.T) {
			compiled := rv64PublicGCExternCompile(t, artifact)
			view := compiled.executionView()
			if !rv64GCExtern(view) || railshotValidateCompiled(view) != nil || view.stagedGCStructProduct() != stagedGCStructExtern {
				t.Fatalf("missing exact gc/extern witness: exact=%t validation=%v product=%v code=%d/%x entries=%v/%v", rv64GCExtern(view), railshotValidateCompiled(view), view.stagedGCStructProduct(), len(view.code), sha256.Sum256(view.code), view.Entry, view.InternalEntry)
			}
			if params, results, err := view.maxCallSlots(); err != nil || params != 3 || results != 1 {
				t.Fatalf("gc/extern call capacity=%d/%d,%v, want 3/1", params, results, err)
			}
			for _, profile := range []struct {
				name string
				gc   GCConfig
			}{{name: "default-gc"}, {name: "stress-gc", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true, StressBarriers: true}}} {
				t.Run(profile.name, func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64I31Config()))
					defer rt.Close()
					mod, err := rt.Module(compiled)
					if err != nil {
						t.Fatal(err)
					}
					in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports()), WithGC(profile.gc))
					mod.Close()
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					state := in.existingGCRefTestTableState()
					if in.gc == nil || state == nil || state.TableCount != 1 || state.Count != 10 || len(state.Descriptors) != 1 || state.Conversion == nil || in.refStore == nil {
						t.Fatalf("collector/extern table state=%v/%+v store=%p", in.gc, state, in.refStore)
					}
					foreign0, err := in.NewExternRef("foreign-0")
					if err != nil {
						t.Fatal(err)
					}
					token0 := ValueExternRef(foreign0).Bits()
					for iteration := 0; iteration < 5; iteration++ {
						if got, err := in.Invoke("init", token0); err != nil || len(got) != 0 {
							t.Fatalf("init iteration %d=%v,%v", iteration, got, err)
						}
					}
					desc := state.Descriptor
					if got := binary.LittleEndian.Uint64(desc[8+1*8:]); got != uint64(nativegc.I31New(7)) {
						t.Fatalf("any table i31=%#x want %#x", got, nativegc.I31New(7))
					}
					for _, index := range []int{2, 3} {
						word := binary.LittleEndian.Uint64(desc[8+index*8:])
						root, err := in.gc.CheckedTableSlot(state.Slots[index])
						if err != nil || word != uint64(root) || !root.IsObj() {
							t.Fatalf("any table object %d=%#x root=%#x err=%v", index, word, root, err)
						}
					}
					foreignAny := binary.LittleEndian.Uint64(desc[8+4*8:])
					if ok, err := state.Conversion.isForeignAny(foreignAny); err != nil || !ok {
						t.Fatalf("table foreign any=%#x,%v/%v", foreignAny, ok, err)
					}
					foreign1, err := in.NewExternRef("foreign-1")
					if err != nil {
						t.Fatal(err)
					}
					token1 := ValueExternRef(foreign1).Bits()
					if got, err := in.Invoke("internalize", token1); err != nil || !slices.Equal(got, []uint64{token1}) {
						t.Fatalf("internalize foreign=%v,%v want %#x", got, err, token1)
					}
					if got, err := in.Invoke("internalize", 0); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("internalize null=%v,%v", got, err)
					}
					foreign2, err := in.NewExternRef("foreign-2")
					if err != nil {
						t.Fatal(err)
					}
					token2 := ValueExternRef(foreign2).Bits()
					if got, err := in.Invoke("externalize", token2); err != nil || !slices.Equal(got, []uint64{token2}) {
						t.Fatalf("externalize foreign=%v,%v want %#x", got, err, token2)
					}
					if got, err := in.Invoke("externalize", 0); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatalf("externalize null=%v,%v", got, err)
					}
					for _, index := range []uint64{0, 5} {
						dirty := uint64(0xfeedface)<<32 | index
						if got, err := in.Invoke("externalize-i", dirty); err != nil || !slices.Equal(got, []uint64{0}) {
							t.Fatalf("externalize-i dirty null index %d=%v,%v", index, got, err)
						}
						if got, err := in.Invoke("externalize-ii", dirty); err != nil || !slices.Equal(got, []uint64{0}) {
							t.Fatalf("externalize-ii dirty null index %d=%v,%v", index, got, err)
						}
					}
					for _, name := range []string{"externalize-i", "externalize-ii"} {
						got, err := in.Invoke(name, uint64(0xfeedface)<<32|10)
						var trap *coreruntime.TrapError
						if got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapTableOutOfBounds {
							t.Fatalf("%s dirty out-of-bounds=%v,%v", name, got, err)
						}
					}
					var publicExtern [4]uint64
					var publicAny [4]uint64
					for i, index := range []uint64{1, 2, 3, 4} {
						dirty := uint64(0xdeadbeef)<<32 | index
						ext, err := in.Invoke("externalize-i", dirty)
						if err != nil || len(ext) != 1 || ext[0] == 0 || ext[0]>>32 == 0 {
							t.Fatalf("externalize-i(%#x)=%v,%v", dirty, ext, err)
						}
						externWord := ext[0]
						any, err := in.Invoke("externalize-ii", dirty)
						if err != nil || len(any) != 1 || any[0] == 0 || any[0]>>32 == 0 {
							t.Fatalf("externalize-ii(%#x)=%v,%v", dirty, any, err)
						}
						anyWord := any[0]
						publicExtern[i], publicAny[i] = externWord, anyWord
						if roundTrip, err := in.Invoke("internalize", externWord); err != nil || !slices.Equal(roundTrip, []uint64{anyWord}) {
							t.Fatalf("round trip %d=%v,%v want %#x", index, roundTrip, err, anyWord)
						}
					}
					if publicExtern[0] == publicAny[0] || publicExtern[1] == publicAny[1] {
						t.Fatal("public extern and any token categories collided")
					}
					if state.Conversion.count != 6 {
						t.Fatalf("conversion entries=%d want 6", state.Conversion.count)
					}
					beforeCount := state.Conversion.count
					if _, err := in.Invoke("externalize", 0xfedcba9876543210); err == nil || !strings.Contains(err.Error(), "invalid anyref token") {
						t.Fatalf("forged public any ingress=%v", err)
					}
					if _, err := in.Invoke("internalize", 0xfedcba9876543210); err == nil || !strings.Contains(err.Error(), "invalid externref token") {
						t.Fatalf("forged public extern ingress=%v", err)
					}
					if state.Conversion.count != beforeCount {
						t.Fatalf("rejected ingress changed conversion count %d -> %d", beforeCount, state.Conversion.count)
					}
					if err := in.gc.CollectFull(nil); err != nil {
						t.Fatal(err)
					}
					if live := in.gc.Stats().LiveObjects; live != 2 {
						t.Fatalf("live objects=%d want struct+array", live)
					}
					for i, index := range []uint64{1, 2, 3, 4} {
						if got, err := in.Invoke("externalize-i", index); err != nil || !slices.Equal(got, []uint64{publicExtern[i]}) {
							t.Fatalf("externalize-i(%d) after GC=%v,%v", index, got, err)
						}
						if got, err := in.Invoke("externalize-ii", index); err != nil || !slices.Equal(got, []uint64{publicAny[i]}) {
							t.Fatalf("externalize-ii(%d) after GC=%v,%v", index, got, err)
						}
					}
				})
			}
		})
	}
}

func TestRV64PublicGCExternAdmissionIsExact(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_EXTERN_ARTIFACT_MODE") == "load" {
		t.Skip("compiler-free runner selected")
	}
	base := rv64PublicGCExternCompile(t, false).executionView()
	for name, mutate := range map[string]func(*Compiled){
		"code":       func(v *Compiled) { v.code = slices.Clone(v.code); v.code[len(v.code)-1] ^= 1 },
		"entry":      func(v *Compiled) { v.Entry[5]++; v.InternalEntry[5]++ },
		"function":   func(v *Compiled) { v.Funcs[4].Results[0] = ValAnyRef },
		"type":       func(v *Compiled) { v.Types[2].Array.Mutable = true },
		"type-key":   func(v *Compiled) { v.FuncTypeID[3] ^= 1 },
		"descriptor": func(v *Compiled) { v.GCTypeDescs[2].ElemSize = 2 },
		"global":     func(v *Compiled) { v.Globals[1].Mutable = true },
		"value-type": func(v *Compiled) { v.ValueTypes[1].Ref.Heap.Abstract = AbstractHeapEq },
		"table":      func(v *Compiled) { v.TableSize++ },
		"export":     func(v *Compiled) { delete(v.Exports, "externalize"); v.Exports["other"] = 3 },
		"feature":    func(v *Compiled) { v.requiredFeatures &^= CoreFeatureGC },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(base)
			candidate.validateMemo = &validateMemo{}
			mutate(candidate)
			if rv64GCExtern(candidate) {
				t.Fatal("hostile retained mutation kept gc/extern witness")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("hostile retained gc/extern mutation admitted", err)
			}
		})
	}
}

func TestRV64PublicGCExternCompilerFreeRejectsSource(t *testing.T) {
	if os.Getenv("WAGO_RV64_GC_EXTERN_ARTIFACT_MODE") != "load" {
		t.Skip("actual compiler-free runtime only")
	}
	c, err := rv64I31Config().Compile(rv64PublicGCExternBytes(t))
	if c != nil {
		_ = c.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		t.Fatal("compiler-free gc/extern path used source compilation", err)
	}
}
