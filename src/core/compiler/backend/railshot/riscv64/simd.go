//go:build riscv64

package riscv64

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// RVV is intentionally not part of the initial RV64G production baseline. The
// module entry point rejects every module whose types, constants, globals,
// locals, or bodies require SIMD before function planning begins. These small
// hooks keep the scalar allocator structurally shared without allowing a v128
// value to reach machine code generation accidentally.
type v128ConstReg struct {
	lo, hi uint64
	reg    Reg
}

func (f *fn) v128ConstMask() regMask                  { return 0 }
func (f *fn) pinnedV128LocalCount() int               { return 0 }
func (f *fn) preloadV128Consts(_ []byte)              {}
func (f *fn) materializeV128(_ *elem) Reg             { panic("riscv64: SIMD reached scalar backend") }
func (f *fn) pushVReg(_ Reg) *elem                    { panic("riscv64: SIMD reached scalar backend") }
func (f *fn) stV128(_ Reg, _ int32, _ Reg)            { panic("riscv64: SIMD reached scalar backend") }
func (f *fn) v128ConstCached(_, _ uint64) (Reg, bool) { return regNone, false }

func (f *fn) emitFD(_ *wasm.Reader) error {
	return fmt.Errorf("riscv64: SIMD requires an RVV-enabled backend")
}
