//go:build linux && !tinygo

package riscv64

import coreruntime "github.com/wago-org/wago/src/core/runtime"

// NativeTailCodeOwner supplies an exact logical image from a trusted runtime
// code owner, already RX and synchronized for process-wide instruction fetch.
// The caller must hold that owner's physical execution lease through native
// execution, parks, resume and cold decoration. A Go pointer or another
// instance's reference to the same cache is not such a lease.
type NativeTailCodeOwner interface {
	NativeTailCodeView() ([]byte, error)
}

// NativeTailCodeImage borrows an immutable published image. It retains its Go
// owner, but neither acquires a physical lease nor seals or unmaps the image.
// It has no serialized representation and accepts no raw code address.
type NativeTailCodeImage struct {
	owner NativeTailCodeOwner
	bytes []byte
	base  uintptr
}

func NewNativeTailCodeImage(owner NativeTailCodeOwner) (*NativeTailCodeImage, error) {
	if owner == nil {
		return nil, unsupported("native tail published code owner")
	}
	b, err := owner.NativeTailCodeView()
	if err != nil {
		return nil, err
	}
	base := tailRAPointer(b)
	if len(b) == 0 || len(b) > MaxCodeBytes || len(b)&3 != 0 || base == 0 || base&15 != 0 || uintptr(len(b)) > ^uintptr(0)-base {
		return nil, unsupported("native tail published code span")
	}
	return &NativeTailCodeImage{owner: owner, bytes: b[:len(b):len(b)], base: base}, nil
}

// Validate must also run after a park, under the reacquired owning lease,
// before an existing image participates in a refreshed target factory.
func (i *NativeTailCodeImage) Validate() error {
	if i == nil || i.owner == nil {
		return unsupported("native tail missing published image")
	}
	b, err := i.owner.NativeTailCodeView()
	if err != nil {
		return err
	}
	if len(b) != len(i.bytes) || tailRAPointer(b) != i.base {
		return unsupported("native tail published image replaced")
	}
	return nil
}

// Base and Bytes describe borrowed RX storage, usable only under the physical
// lease and after Validate. Bytes must never be written by a consumer.
func (i *NativeTailCodeImage) Base() uintptr {
	if i == nil {
		return 0
	}
	return i.base
}
func (i *NativeTailCodeImage) Bytes() []byte {
	if i == nil {
		return nil
	}
	return i.bytes
}

type tailRACodeKey struct {
	buffer *coreruntime.CodeBuffer
	base   uintptr
	bytes  int
}

func (f tailRAFunction) codeKey() tailRACodeKey {
	if f.published != nil {
		return tailRACodeKey{base: f.published.Base(), bytes: len(f.published.Bytes())}
	}
	return tailRACodeKey{buffer: f.code}
}
func (f tailRAFunction) codeBase() uintptr {
	if f.published != nil {
		return f.published.Base()
	}
	return f.code.Base()
}
func (f tailRAFunction) codeBytes() []byte {
	if f.published != nil {
		return f.published.Bytes()
	}
	return f.code.Bytes()
}
func (f tailRAFunction) validateCode() error {
	if (f.code == nil) == (f.published == nil) {
		return unsupported("native tail exact code owner")
	}
	if f.published != nil {
		return f.published.Validate()
	}
	return nil
}
func (f tailRAFunction) publishCode() error {
	if err := f.validateCode(); err != nil {
		return err
	}
	if f.published != nil {
		return nil // Publication belongs to the borrowed runtime owner.
	}
	return f.code.Seal()
}
