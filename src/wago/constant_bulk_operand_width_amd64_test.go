//go:build amd64 && !tinygo

package wago

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type constantBulkOp uint8

const (
	constantBulkCopyDst constantBulkOp = iota
	constantBulkCopySrc
	constantBulkFillDst
)

func (op constantBulkOp) String() string {
	switch op {
	case constantBulkCopyDst:
		return "copy-dst"
	case constantBulkCopySrc:
		return "copy-src"
	case constantBulkFillDst:
		return "fill-dst"
	default:
		return fmt.Sprintf("constantBulkOp(%d)", op)
	}
}

func constantBulkAddress(op constantBulkOp, roundTrip bool) []byte {
	out := []byte{0x10, 0x00} // call env.addr
	if roundTrip {
		out = append(out, 0x21, 0x00, 0x20, 0x00) // local.set 0; local.get 0
	}
	return out
}

func constantBulkFunction(op constantBulkOp, n uint32, roundTrip bool) []byte {
	locals := []byte{0x00}
	if roundTrip {
		locals = []byte{0x01, 0x01, 0x7f} // one i32 local
	}
	body := append([]byte(nil), locals...)
	addr := constantBulkAddress(op, roundTrip)
	switch op {
	case constantBulkCopyDst:
		body = append(body, addr...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(64)...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(n))...)
		body = append(body, 0xfc, 0x0a, 0x00, 0x00) // memory.copy 0 0
	case constantBulkCopySrc:
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(128)...)
		body = append(body, addr...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(n))...)
		body = append(body, 0xfc, 0x0a, 0x00, 0x00)
	case constantBulkFillDst:
		body = append(body, addr...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(0x6b)...)
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(n))...)
		body = append(body, 0xfc, 0x0b, 0x00) // memory.fill 0
	}
	if n == 0 {
		body = append(body, 0x41, 0x01) // successful zero-length operation
	} else {
		checkAt := int32(0)
		if op == constantBulkCopySrc {
			checkAt = 128
		}
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(checkAt)...)
		body = append(body, 0x2d, 0x00, 0x00) // i32.load8_u
	}
	body = append(body, 0x0b)
	return append(wasmtest.ULEB(uint32(len(body))), body...)
}

func constantBulkDirtyAddressModule(op constantBulkOp, n uint32) []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("addr")...), 0x00, 0x00)
	data := make([]byte, 64)
	for i := range data {
		data[i] = 0x5a
	}
	segment := []byte{0x00, 0x41}
	segment = append(segment, wasmtest.SLEB32(64)...)
	segment = append(segment, 0x0b)
	segment = append(segment, wasmtest.ULEB(uint32(len(data)))...)
	segment = append(segment, data...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("direct", 0, 1),
			wasmtest.ExportEntry("local", 0, 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			constantBulkFunction(op, n, false),
			constantBulkFunction(op, n, true),
		)),
		wasmtest.Section(11, wasmtest.Vec(segment)),
	)
}

func constantBulkConfigs() []struct {
	name string
	cfg  *RuntimeConfig
} {
	out := []struct {
		name string
		cfg  *RuntimeConfig
	}{{"explicit", NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)}}
	if guardPageBuilt {
		out = append(out, struct {
			name string
			cfg  *RuntimeConfig
		}{"guard", NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)})
	}
	return out
}

func invokeConstantBulk(t testing.TB, cfg *RuntimeConfig, op constantBulkOp, n, low uint32, export string) ([]uint64, error) {
	t.Helper()
	compiled, err := Compile(cfg, constantBulkDirtyAddressModule(op, n))
	if err != nil {
		t.Fatalf("compile %s n=%d: %v", op, n, err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.addr": HostFunc(func(_ HostModule, _, results []uint64) {
			results[0] = 0xdead_beef_0000_0000 | uint64(low)
		}),
	}})
	if err != nil {
		t.Fatalf("instantiate %s n=%d: %v", op, n, err)
	}
	defer in.Close()
	return in.Invoke(export)
}

func TestConstantBulkMemory32CanonicalizesDirtyHostAddresses(t *testing.T) {
	lengths := []uint32{0, 1, 2, 3, 7, 8, 16, 31, 32, 33, 63, 64}
	ops := []constantBulkOp{constantBulkCopyDst, constantBulkCopySrc, constantBulkFillDst}
	for _, mode := range constantBulkConfigs() {
		for _, op := range ops {
			low := uint32(0)
			want := int32(0x5a)
			if op == constantBulkCopySrc {
				low = 64
			}
			if op == constantBulkFillDst {
				want = 0x6b
			}
			for _, n := range lengths {
				for _, export := range []string{"direct", "local"} {
					t.Run(fmt.Sprintf("%s/%s/n=%d/%s", mode.name, op, n, export), func(t *testing.T) {
						got, err := invokeConstantBulk(t, mode.cfg, op, n, low, export)
						if err != nil {
							t.Fatalf("normal Wasm operation trapped: %v", err)
						}
						expect := want
						if n == 0 {
							expect = 1
						}
						if len(got) != 1 || AsI32(got[0]) != expect {
							t.Fatalf("result = %v, want %d", got, expect)
						}
					})
				}
			}
		}
	}
}

func TestConstantBulkZeroLengthStillChecksOffsets(t *testing.T) {
	ops := []constantBulkOp{constantBulkCopyDst, constantBulkCopySrc, constantBulkFillDst}
	for _, mode := range constantBulkConfigs() {
		for _, op := range ops {
			for _, export := range []string{"direct", "local"} {
				t.Run(fmt.Sprintf("%s/%s/exact-end/%s", mode.name, op, export), func(t *testing.T) {
					got, err := invokeConstantBulk(t, mode.cfg, op, 0, 65536, export)
					if err != nil || len(got) != 1 || AsI32(got[0]) != 1 {
						t.Fatalf("exact-end zero length = %v, %v; want success", got, err)
					}
				})
				t.Run(fmt.Sprintf("%s/%s/one-past/%s", mode.name, op, export), func(t *testing.T) {
					_, err := invokeConstantBulk(t, mode.cfg, op, 0, 65537, export)
					if err == nil || !strings.Contains(err.Error(), "out of bounds") {
						t.Fatalf("one-past zero length trap = %v", err)
					}
				})
				t.Run(fmt.Sprintf("%s/%s/nonzero-oob/%s", mode.name, op, export), func(t *testing.T) {
					_, err := invokeConstantBulk(t, mode.cfg, op, 1, 65536, export)
					if err == nil || !strings.Contains(err.Error(), "out of bounds") {
						t.Fatalf("ordinary nonzero out-of-bounds trap = %v", err)
					}
				})
			}
		}
	}
}

func BenchmarkConstantBulkMemory32(b *testing.B) {
	for _, op := range []constantBulkOp{constantBulkCopyDst, constantBulkFillDst} {
		b.Run(op.String()+"/compile", func(b *testing.B) {
			module := constantBulkDirtyAddressModule(op, 64)
			cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				compiled, err := Compile(cfg, module)
				if err != nil {
					b.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
