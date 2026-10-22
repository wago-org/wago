//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type tailPublishedOwnerForTest struct {
	view []byte
	err  error
}

func (o *tailPublishedOwnerForTest) NativeTailCodeView() ([]byte, error) {
	if o == nil {
		return nil, ErrUnsupported
	}
	return o.view, o.err
}

func tailPublishedFunctionForTest(t *testing.T, f tailRAFunction) (tailRAFunction, *tailPublishedOwnerForTest) {
	t.Helper()
	if err := f.code.Seal(); err != nil {
		t.Fatal(err)
	}
	o := &tailPublishedOwnerForTest{view: f.code.Bytes()}
	image, err := NewNativeTailCodeImage(o)
	if err != nil {
		t.Fatal(err)
	}
	f.published, f.code = image, nil
	return f, o
}

func TestNativeTailPublishedCodeExactBorrowedSpan(t *testing.T) {
	f := tailRAProofFunctionFixture(t, &wasm.CompType{Kind: wasm.CompFunc}, abi.RV64InstanceContextBytes)
	buffer := f.code
	f, owner := tailPublishedFunctionForTest(t, f)
	if err := f.validate(true); err != nil || f.published.Base() != buffer.Base() || !bytes.Equal(f.published.Bytes(), buffer.Bytes()) {
		t.Fatal("published retained framing", err)
	}
	if n := testing.AllocsPerRun(4, func() {
		if err := f.published.Validate(); err != nil {
			panic(err)
		}
	}); n != 0 {
		t.Fatal("published warm span validation allocations", n)
	}
	other, err := NewNativeTailCodeImage(owner)
	if err != nil {
		t.Fatal(err)
	}
	alias := f
	alias.published = other
	if alias.codeKey() != f.codeKey() {
		t.Fatal("duplicate views of one logical image counted separately")
	}
	for _, tc := range []struct {
		name string
		edit func(*tailRAFunction)
	}{
		{"both", func(v *tailRAFunction) { v.code = buffer }},
		{"neither", func(v *tailRAFunction) { v.published = nil }},
		{"frame", func(v *tailRAFunction) { v.info.FrameBytes += 16 }},
		{"snapshot", func(v *tailRAFunction) { v.snapshot = bytes.Clone(v.snapshot); v.snapshot[0] ^= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := f
			tc.edit(&v)
			if err := v.validate(true); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid borrowed framing accepted", err)
			}
		})
	}
	view := owner.view
	for _, bad := range [][]byte{nil, view[:len(view)-4], view[16:], bytes.Clone(view)} {
		owner.view = bad
		if err := f.published.Validate(); !errors.Is(err, ErrUnsupported) {
			t.Fatal("replaced borrowed span accepted", err)
		}
	}
	owner.view = view
	owner.err = errors.New("lease retired")
	if err := f.publishCode(); !errors.Is(err, owner.err) {
		t.Fatal("publication bypassed borrowed lease check", err)
	}
	owner.err = nil
	if err := f.publishCode(); err != nil || buffer.Base() == 0 {
		t.Fatal("borrowed publication changed actual owner", err)
	}
}

func TestNativeTailPublishedCodeRejectsInvalidViews(t *testing.T) {
	b := tailRAProofBuffer(t, bytes.Repeat([]byte{0x13, 0, 0, 0}, 8))
	if err := b.Seal(); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []NativeTailCodeOwner{nil, (*tailPublishedOwnerForTest)(nil), &tailPublishedOwnerForTest{}, &tailPublishedOwnerForTest{view: b.Bytes()[4:]}, &tailPublishedOwnerForTest{view: b.Bytes()[:31]}} {
		if image, err := NewNativeTailCodeImage(owner); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid published owner/span", image, err)
		}
	}
}
