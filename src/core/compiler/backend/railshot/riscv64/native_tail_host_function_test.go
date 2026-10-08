//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestNativeTailHostCertificatePublishedIdentity(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, cells := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {4, 4}, {64, 64}} {
			t.Run(fmt.Sprintf("owned%t-params%d-results%d", owned, cells[0], cells[1]), func(t *testing.T) {
				const index uint32 = 0xfedcba98
				code, cert, err := NativeTailHostThunkWithCertificate(index, cells[0], cells[1], owned)
				if err != nil {
					t.Fatal(err)
				}
				plain := HostIndirectSyncThunk(index, cells[0], cells[1])
				if owned {
					plain = HostIndirectOwnedSyncThunk(index, cells[0], cells[1])
				}
				if !bytes.Equal(plain, code) || cert.CodeBytes() != len(code) {
					t.Fatal("certificate changed wrapper bytes")
				}
				// Nonzero aligned offsets ensure neither preceding bytes nor page padding
				// become part of the certificate's logical wrapper extent.
				prefix := bytes.Repeat([]byte{0x13, 0, 0, 0}, 4)
				buffer := tailRAProofBuffer(t, append(bytes.Clone(prefix), code...))
				if err := buffer.Seal(); err != nil {
					t.Fatal(err)
				}
				lease := &tailPublishedOwnerForTest{view: buffer.Bytes()}
				image, err := NewNativeTailCodeImage(lease)
				if err != nil {
					t.Fatal(err)
				}
				if err := cert.Validate(image, 16, index, cells[0], cells[1], owned); err != nil {
					t.Fatal(err)
				}
				if n := testing.AllocsPerRun(4, func() {
					if err := cert.Validate(image, 16, index, cells[0], cells[1], owned); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm host certificate allocations", n)
				}
				for _, offset := range []int{-16, 0, 4, len(buffer.Bytes())} {
					if err := cert.Validate(image, offset, index, cells[0], cells[1], owned); !errors.Is(err, ErrUnsupported) {
						t.Fatal("incorrect wrapper span", offset, err)
					}
				}
				for _, args := range []struct {
					index uint32
					p, r  int
					owned bool
				}{{index + 1, cells[0], cells[1], owned}, {index, cells[0] + 1, cells[1], owned}, {index, cells[0], cells[1] + 1, owned}, {index, cells[0], cells[1], !owned}} {
					if err := cert.Validate(image, 16, args.index, args.p, args.r, args.owned); !errors.Is(err, ErrUnsupported) {
						t.Fatal("incorrect host resolver identity", err)
					}
				}
				wrong := bytes.Clone(buffer.Bytes())
				wrong[len(wrong)-4] ^= 4
				other := tailRAProofBuffer(t, wrong)
				if err := other.Seal(); err != nil {
					t.Fatal(err)
				}
				changed, err := NewNativeTailCodeImage(&tailPublishedOwnerForTest{view: other.Bytes()})
				if err != nil {
					t.Fatal(err)
				}
				if err := cert.Validate(changed, 16, index, cells[0], cells[1], owned); !errors.Is(err, ErrUnsupported) {
					t.Fatal("unissued wrapper accepted", err)
				}
				lease.err = errors.New("retired host wrapper owner")
				if err := cert.Validate(image, 16, index, cells[0], cells[1], owned); !errors.Is(err, lease.err) {
					t.Fatal("retired image accessed", err)
				}
			})
		}
	}
	for _, cells := range [][2]int{{-1, 0}, {0, -1}, {65, 0}, {0, 65}, {1 << 30, 1 << 30}} {
		if b, c, err := NativeTailHostThunkWithCertificate(0, cells[0], cells[1], false); b != nil || c != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("unbounded host certificate issued", err)
		}
	}
	if n := (*NativeTailHostCertificate)(nil).CodeBytes(); n != 0 {
		t.Fatal("nil certificate extent", n)
	}
}

func TestNativeTailHostFunctionFrozenPhysicalOwner(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned%t", owned), func(t *testing.T) {
			sig := wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}, Results: []wasm.ValType{wasm.I32, wasm.F32}}
			source := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
			binary.LittleEndian.PutUint64(source.directory[16:], uint64(source.job.LinMemBase()))
			b, cert, err := NativeTailHostThunkWithCertificate(37, len(sig.Params), len(sig.Results), owned)
			if err != nil {
				t.Fatal(err)
			}
			code := tailRAProofBuffer(t, b)
			if err := code.Seal(); err != nil {
				t.Fatal(err)
			}
			lease := &tailPublishedOwnerForTest{view: code.Bytes()}
			image, err := NewNativeTailCodeImage(lease)
			if err != nil {
				t.Fatal(err)
			}
			f, err := NewNativeTailHostFunction(image, cert, source.job, source.context, source.directory, 0, 37, owned, sig)
			if err != nil {
				t.Fatal(err)
			}
			sig.Params[0] = wasm.F64
			sig.Results[0] = wasm.F64
			if !wasm.EqualValType(f.signature.Params[0], wasm.I32) || !wasm.EqualValType(f.signature.Results[0], wasm.I32) {
				t.Fatal("mutable host signature retained")
			}
			if a := testing.AllocsPerRun(4, func() {
				if err := f.Validate(); err != nil {
					panic(err)
				}
			}); a != 0 {
				t.Fatal("warm host owner allocations", a)
			}
			for _, tc := range []struct {
				name string
				edit func(*NativeTailHostFunction)
			}{
				{"image", func(f *NativeTailHostFunction) { f.image = nil }},
				{"certificate", func(f *NativeTailHostFunction) { f.certificate = nil }},
				{"job", func(f *NativeTailHostFunction) { f.job = nil }},
				{"context", func(f *NativeTailHostFunction) { f.context = f.context[:128] }},
				{"anchor", func(f *NativeTailHostFunction) { f.anchor = f.anchor[:32] }},
				{"offset", func(f *NativeTailHostFunction) { f.offset = 16 }},
				{"resolver-index", func(f *NativeTailHostFunction) { f.dispatchIndex++ }},
				{"ownership-mode", func(f *NativeTailHostFunction) { f.owned = !f.owned }},
				{"signature-count", func(f *NativeTailHostFunction) { f.signature = &wasm.CompType{Kind: wasm.CompFunc} }},
				{"vector", func(f *NativeTailHostFunction) {
					f.signature = &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.V128}}
				}},
				{"reference", func(f *NativeTailHostFunction) {
					f.signature = &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					v := *f
					tc.edit(&v)
					if err := v.Validate(); !errors.Is(err, ErrUnsupported) {
						t.Fatal("invalid host target accepted", err)
					}
				})
			}
			anchor := bytes.Clone(source.directory)
			for _, off := range []int{16, 32} {
				binary.LittleEndian.PutUint64(source.directory[off:], binary.LittleEndian.Uint64(source.directory[off:])+8)
				if err := f.Validate(); !errors.Is(err, ErrUnsupported) {
					t.Fatal("changed host anchor", off, err)
				}
				copy(source.directory, anchor)
			}
			// A wrapper does not acquire the production guest frame contract.
			if _, err := NewNativeTailFunction(image, source.job, source.context, source.directory, 0, len(b), 0, *f.signature); !errors.Is(err, ErrUnsupported) {
				t.Fatal("host wrapper accepted as genuine guest frame", err)
			}
			lease.err = errors.New("physical host retirement")
			f.context = nil
			if err := f.Validate(); !errors.Is(err, lease.err) {
				t.Fatal("owner checked context before image lifetime", err)
			}
		})
	}
	for _, n := range []int{-1, 129, 1 << 30} {
		if _, err := NativeTailHostFunctionBytes(n); !errors.Is(err, ErrUnsupported) {
			t.Fatal("host metadata bound", n, err)
		}
	}
}
