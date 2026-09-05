// Package amd64 is the x86-64 instruction encoder (the Asm type) that the code
// generator in backend/railshot drives to emit machine code. It holds only the
// encoder, the Reg/Cond vocabulary, and the CompiledModule result type; the
// wasm→native code generator itself lives in backend/railshot.
package amd64

import (
	"encoding/binary"
	"unsafe"
)

type Reg byte

const (
	RAX Reg = 0
	RCX Reg = 1
	RDX Reg = 2
	RBX Reg = 3
	RSP Reg = 4
	RBP Reg = 5
	RSI Reg = 6
	RDI Reg = 7
	R8  Reg = 8
	R9  Reg = 9
	R10 Reg = 10
	R11 Reg = 11
	R12 Reg = 12
	R13 Reg = 13
	R14 Reg = 14
	R15 Reg = 15
)

type Asm struct {
	B                            []byte
	EncodingStats                *EncodingStats
	Rel32Sites                   []Rel32Site
	Rel32SiteLimit               int
	Rel32Count                   uint32
	UsesBMI2                     bool
	Rel32Overflow                bool
	CompactAccumulatorImmediates bool
	LocalRefs                    *LocalRefRecorder
	rel32Inline                  [2]Rel32Site
}

// LocalRefSite identifies one disp32 field emitted for a symbolic Wasm local
// home. ModRMOff and DispOff are maximal-encoding offsets; the backend may
// rewrite the displacement and delete its trailing bytes during finalization.
type LocalRefSite struct {
	ModRMOff uint32
	DispOff  uint32
	Local    uint32
	OldDisp  int32
}

// LocalRefRecorder is reusable bounded scratch for symbolic local-home memory
// references. The backend retains exact per-local emitted reference counts;
// Sites stores only disp32 forms that can shrink after a safe slot swap.
type LocalRefRecorder struct {
	Sites    []LocalRefSite
	Limit    int
	Locals   uint32
	Next     uint32
	Pending  bool
	Overflow bool
}

// LocalRefScratchSize is the caller-owned storage required for capacity records
// at any byte-slice alignment. The alignment slop is compiler scratch only.
func LocalRefScratchSize(capacity int) int { return capacity*int(unsafe.Sizeof(LocalRefSite{})) + 7 }

// BindStorage binds the recorder to pointer-free caller-owned scratch. The
// caller keeps storage alive and does not overwrite it until finalization.
func (r *LocalRefRecorder) BindStorage(storage []byte, capacity int) bool {
	if capacity <= 0 || len(storage) == 0 {
		return false
	}
	bytes := capacity * int(unsafe.Sizeof(LocalRefSite{}))
	address := uintptr(unsafe.Pointer(&storage[0]))
	start := int(-address & 7)
	if start+bytes > len(storage) {
		return false
	}
	records := unsafe.Slice((*LocalRefSite)(unsafe.Pointer(&storage[start])), capacity)
	r.Sites = records[:0]
	return true
}

// BindLocalRefTail reserves the uncommitted end of B for local-reference
// records. It mirrors BindRel32Tail so serial codegen can share its executable
// arena with both bounded finalizer inventories without a heap allocation.
func (a *Asm) BindLocalRefTail(r *LocalRefRecorder, capacity int) bool {
	if capacity <= 0 {
		return false
	}
	bytes := capacity * int(unsafe.Sizeof(LocalRefSite{}))
	start := (cap(a.B) - bytes) &^ 7
	if start < len(a.B) || start < 0 {
		return false
	}
	full := a.B[:cap(a.B)]
	records := unsafe.Slice((*LocalRefSite)(unsafe.Pointer(&full[start])), capacity)
	a.B = a.B[:len(a.B):start]
	r.Sites = records[:0]
	return true
}

// Reset prepares already-bound storage for one function. It never allocates.
func (r *LocalRefRecorder) Reset(nLocals, limit int) bool {
	if limit <= 0 || cap(r.Sites) < limit {
		r.Sites = r.Sites[:0]
		r.Limit = 0
		r.Locals = 0
		r.Next = 0
		r.Pending = false
		r.Overflow = false
		return false
	}
	r.Sites = r.Sites[:0]
	r.Limit = limit
	r.Locals = uint32(nLocals)
	r.Next = 0
	r.Pending = false
	r.Overflow = false
	return true
}

func (r *LocalRefRecorder) Mark(local uint32) {
	if r.Pending || local >= r.Locals {
		r.Overflow = true
	}
	r.Next = local
	r.Pending = true
}

// EncodingStats records exact memory-displacement choices made by the encoder.
// It is optional so ordinary code generation does not allocate. Frame counts
// are the subset whose base register is RSP.
type EncodingStats struct {
	MemoryDisp0  uint64 `json:"memory_disp0"`
	MemoryDisp8  uint64 `json:"memory_disp8"`
	MemoryDisp32 uint64 `json:"memory_disp32"`
	FrameDisp0   uint64 `json:"frame_disp0"`
	FrameDisp8   uint64 `json:"frame_disp8"`
	FrameDisp32  uint64 `json:"frame_disp32"`
	LocalDisp0   uint64 `json:"local_disp0"`
	LocalDisp8   uint64 `json:"local_disp8"`
	LocalDisp32  uint64 `json:"local_disp32"`
	RexPrefixes  uint64 `json:"rex_prefixes"`
	RexWPrefixes uint64 `json:"rex_w_prefixes"`
	RexBare      uint64 `json:"rex_bare_prefixes"`
	MovImm32     uint64 `json:"mov_imm32"`
	MovImm32Sext uint64 `json:"mov_imm32_sign_extended"`
	MovImm64     uint64 `json:"mov_imm64"`
	MovImmNarrow uint64 `json:"mov_imm64_narrowed"`
	MovImmSaved  uint64 `json:"mov_imm64_bytes_saved"`
	ShiftImmZero uint64 `json:"shift_imm_zero_elided"`
	ShiftImmOne  uint64 `json:"shift_imm_one"`
	ShiftImm8    uint64 `json:"shift_imm8"`
	ShiftSaved   uint64 `json:"shift_imm_bytes_saved"`
	AluImm32Acc  uint64 `json:"alu_imm32_accumulator"`
	TestImm32Acc uint64 `json:"test_imm32_accumulator"`
}

// Add accumulates another encoder histogram.
func (s *EncodingStats) Add(other EncodingStats) {
	if s == nil {
		return
	}
	s.MemoryDisp0 += other.MemoryDisp0
	s.MemoryDisp8 += other.MemoryDisp8
	s.MemoryDisp32 += other.MemoryDisp32
	s.FrameDisp0 += other.FrameDisp0
	s.FrameDisp8 += other.FrameDisp8
	s.FrameDisp32 += other.FrameDisp32
	s.LocalDisp0 += other.LocalDisp0
	s.LocalDisp8 += other.LocalDisp8
	s.LocalDisp32 += other.LocalDisp32
	s.RexPrefixes += other.RexPrefixes
	s.RexWPrefixes += other.RexWPrefixes
	s.RexBare += other.RexBare
	s.MovImm32 += other.MovImm32
	s.MovImm32Sext += other.MovImm32Sext
	s.MovImm64 += other.MovImm64
	s.MovImmNarrow += other.MovImmNarrow
	s.MovImmSaved += other.MovImmSaved
	s.ShiftImmZero += other.ShiftImmZero
	s.ShiftImmOne += other.ShiftImmOne
	s.ShiftImm8 += other.ShiftImm8
	s.ShiftSaved += other.ShiftSaved
	s.AluImm32Acc += other.AluImm32Acc
	s.TestImm32Acc += other.TestImm32Acc
}

// MemoryDisplacementBytes returns the exact bytes occupied by recorded disp8
// and disp32 fields. FrameDisplacementBytes is the RSP-based subset.
func (s EncodingStats) MemoryDisplacementBytes() uint64 {
	return s.MemoryDisp8 + 4*s.MemoryDisp32
}

func (s EncodingStats) FrameDisplacementBytes() uint64 {
	return s.FrameDisp8 + 4*s.FrameDisp32
}

// RecordLocalFrameAddress attributes one emitted RSP-relative memory operand
// to a reorderable Wasm local home. The backend calls this at the semantic
// local seam; frame headers, EH records, and spill slots remain excluded.
func (s *EncodingStats) RecordLocalFrameAddress(disp int32) {
	if disp == 0 {
		s.LocalDisp0++
	} else if disp >= -128 && disp <= 127 {
		s.LocalDisp8++
	} else {
		s.LocalDisp32++
	}
}

func (s EncodingStats) LocalFrameDisplacementBytes() uint64 {
	return s.LocalDisp8 + 4*s.LocalDisp32
}

// RexNonWExtensionPrefixes is the upper bound of prefixes removable solely by
// keeping operands in the low register bank. REX.W and bare byte-register REX
// prefixes remain necessary independent of high-register assignment.
func (s EncodingStats) RexNonWExtensionPrefixes() uint64 {
	return s.RexPrefixes - s.RexWPrefixes - s.RexBare
}

func (a *Asm) recordAddress(base Reg, mod byte) {
	s := a.EncodingStats
	if s == nil {
		return
	}
	switch mod {
	case 0x00:
		s.MemoryDisp0++
		if base == RSP {
			s.FrameDisp0++
		}
	case 0x40:
		s.MemoryDisp8++
		if base == RSP {
			s.FrameDisp8++
		}
	case 0x80:
		s.MemoryDisp32++
		if base == RSP {
			s.FrameDisp32++
		}
	}
}

func (a *Asm) recordRipAddress() {
	if a.EncodingStats != nil {
		a.EncodingStats.MemoryDisp32++
	}
}

// Rel32Count records explicitly emitted function-local PC-relative
// displacements. Initial frame compaction admits only functions with none; a
// later bounded site inventory can retain their exact offsets for relaxation.
type Rel32Site struct {
	atAndFlags uint32
}

// Rel32Kind identifies the explicitly emitted sites whose maximal branch form
// may be shortened by a bounded finalizer. Other rel32 users, including calls
// and RIP-relative addresses, remain relocatable but retain their width.
type Rel32Kind uint8

const (
	Rel32Other Rel32Kind = iota
	Rel32Jmp
	Rel32Jcc
)

const (
	rel32OffsetBits = 29
	rel32OffsetMask = uint32(1<<rel32OffsetBits - 1)
	rel32ShortFlag  = uint32(1 << rel32OffsetBits)
	rel32KindShift  = rel32OffsetBits + 1
)

func (s Rel32Site) At() int         { return int(s.atAndFlags & rel32OffsetMask) }
func (s Rel32Site) Kind() Rel32Kind { return Rel32Kind(s.atAndFlags >> rel32KindShift) }
func (s Rel32Site) Short() bool     { return s.atAndFlags&rel32ShortFlag != 0 }
func (s *Rel32Site) SetShort(short bool) {
	if short {
		s.atAndFlags |= rel32ShortFlag
	} else {
		s.atAndFlags &^= rel32ShortFlag
	}
}

// Rel32ScratchSize is the tail capacity required to bind capacity packed
// records at any byte-slice alignment. The alignment slop is never committed as
// code.
func Rel32ScratchSize(capacity int) int { return capacity*int(unsafe.Sizeof(Rel32Site{})) + 7 }

// ResetRel32Recorder retains external/tail storage when present and otherwise
// uses two records from padding already available in Asm. The inline fallback
// covers common tiny functions without a heap allocation.
func (a *Asm) ResetRel32Recorder(limit int) {
	a.Rel32Count = 0
	a.Rel32SiteLimit = limit
	a.Rel32Overflow = false
	if limit == 0 {
		a.Rel32Sites = nil
	} else if cap(a.Rel32Sites) > len(a.rel32Inline) {
		a.Rel32Sites = a.Rel32Sites[:0]
	} else {
		a.Rel32Sites = a.rel32Inline[:0]
	}
}

// BindRel32Storage binds records to caller-owned, pointer-free scratch. The
// caller keeps storage alive and does not overwrite it until finalization.
func (a *Asm) BindRel32Storage(storage []byte, capacity int) bool {
	if capacity <= 0 || len(storage) == 0 {
		return false
	}
	bytes := capacity * int(unsafe.Sizeof(Rel32Site{}))
	address := uintptr(unsafe.Pointer(&storage[0]))
	start := int(-address & 7)
	if start+bytes > len(storage) {
		return false
	}
	records := unsafe.Slice((*Rel32Site)(unsafe.Pointer(&storage[start])), capacity)
	a.Rel32Sites = records[:0]
	return true
}

// BindRel32Tail uses the uncommitted end of B as bounded compiler scratch and
// caps B before that region, so subsequent instruction appends cannot overwrite
// records. B must refer to writable, pointer-free storage owned for the complete
// function compile. The finalizer consumes the records before the code prefix is
// committed or sealed.
func (a *Asm) BindRel32Tail(capacity int) bool {
	if capacity <= 0 {
		return false
	}
	bytes := capacity * int(unsafe.Sizeof(Rel32Site{}))
	start := (cap(a.B) - bytes) &^ 7
	if start < len(a.B) || start < 0 {
		return false
	}
	full := a.B[:cap(a.B)]
	storage := unsafe.Slice((*Rel32Site)(unsafe.Pointer(&full[start])), capacity)
	a.B = a.B[:len(a.B):start]
	a.Rel32Sites = storage[:0]
	return true
}

// Grow ensures B has capacity for at least n bytes, reusing the existing backing
// array when it is already large enough. Used to pre-size a reused encoder buffer
// per function so ordinary emits don't repeatedly re-grow the slice.
func (a *Asm) Grow(n int) {
	if cap(a.B) < n {
		b := make([]byte, len(a.B), n)
		copy(b, a.B)
		a.B = b
	}
}

func (a *Asm) emit(bs ...byte) { a.B = append(a.B, bs...) }
func (a *Asm) imm32(v int32) {
	a.B = append(a.B, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
func (a *Asm) Len() int                  { return len(a.B) }
func (a *Asm) PatchU32(at int, v uint32) { binary.LittleEndian.PutUint32(a.B[at:], v) }

func (a *Asm) rexPrefix(prefix byte) byte {
	if a.EncodingStats != nil {
		a.EncodingStats.RexPrefixes++
		if prefix&0x08 != 0 {
			a.EncodingStats.RexWPrefixes++
		}
		if prefix == 0x40 {
			a.EncodingStats.RexBare++
		}
	}
	return prefix
}

func (a *Asm) rex(w, r, x, b bool) byte {
	v := byte(0x40)
	if w {
		v |= 0x08
	}
	if r {
		v |= 0x04
	}
	if x {
		v |= 0x02
	}
	if b {
		v |= 0x01
	}
	return a.rexPrefix(v)
}

// addrMode selects the shortest ModRM displacement form. With mod=00, direct
// ModRM r/m=101 denotes RIP-relative addressing, while SIB base=101 denotes no
// base. RBP/R13 therefore use an explicit zero disp8 in either form.
func addrMode(base Reg, disp int32) byte {
	if disp == 0 && base&7 != 5 {
		return 0x00
	}
	if disp >= -128 && disp <= 127 {
		return 0x40
	}
	return 0x80
}

func (a *Asm) emitDisp(mod byte, disp int32) {
	switch mod {
	case 0x40:
		a.emit(byte(disp))
	case 0x80:
		a.imm32(disp)
	}
}

// baseAddr emits ModRM, the required base-only SIB for RSP/R12, and the
// shortest displacement selected by addrMode.
func (a *Asm) baseAddr(regField byte, base Reg, disp int32) {
	mod := addrMode(base, disp)
	a.recordAddress(base, mod)
	modRMOff := len(a.B)
	rm := byte(base & 7)
	if rm == 4 {
		a.emit(mod | ((regField & 7) << 3) | 0x04)
		a.emit(0x24) // scale=0, index=none, base=RSP/R12
	} else {
		a.emit(mod | ((regField & 7) << 3) | rm)
	}
	a.recordLocalRef(base, mod, modRMOff, len(a.B), disp)
	a.emitDisp(mod, disp)
}

func (a *Asm) recordLocalRef(base Reg, mod byte, modRMOff, dispOff int, disp int32) {
	r := a.LocalRefs
	if r == nil || !r.Pending {
		return
	}
	local := r.Next
	r.Pending = false
	if base != RSP || local >= r.Locals {
		r.Overflow = true
		return
	}
	if mod != 0x80 {
		return
	}
	if modRMOff < 0 || dispOff < 0 || uint64(dispOff) > uint64(^uint32(0)) {
		r.Overflow = true
		return
	}
	if len(r.Sites) >= r.Limit {
		return
	}
	r.Sites = append(r.Sites, LocalRefSite{ModRMOff: uint32(modRMOff), DispOff: uint32(dispOff), Local: local, OldDisp: disp})
}

func (a *Asm) memOp(opcode byte, regField byte, base Reg, disp int32, w bool) {
	rb := base >= 8
	rr := regField >= 8
	if w || rr || rb {
		a.emit(a.rex(w, rr, false, rb))
	}
	a.emit(opcode)
	a.baseAddr(regField, base, disp)
}

func (a *Asm) Push(r Reg) {
	if r >= 8 {
		a.emit(a.rexPrefix(0x41))
	}
	a.emit(0x50 | byte(r&7))
}

func (a *Asm) Pop(r Reg) {
	if r >= 8 {
		a.emit(a.rexPrefix(0x41))
	}
	a.emit(0x58 | byte(r&7))
}

func (a *Asm) MovImm32(r Reg, v int32) {
	if a.EncodingStats != nil {
		a.EncodingStats.MovImm32++
	}
	if r >= 8 {
		a.emit(a.rexPrefix(0x41))
	}
	a.emit(0xB8 | byte(r&7))
	a.imm32(v)
}

func (a *Asm) MovRegReg32(dst, src Reg) {
	rr := src >= 8
	rb := dst >= 8
	if rr || rb {
		a.emit(a.rex(false, rr, false, rb))
	}
	a.emit(0x89)
	a.emit(0xC0 | ((byte(src) & 7) << 3) | byte(dst&7))
}

func (a *Asm) sseBitOp(opcode byte, dst, src Reg, w bool) {
	a.emit(0xF3)
	if w || dst >= 8 || src >= 8 {
		a.emit(a.rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, opcode, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

func (a *Asm) Lzcnt(dst, src Reg, w bool)  { a.sseBitOp(0xBD, dst, src, w) }
func (a *Asm) Tzcnt(dst, src Reg, w bool)  { a.sseBitOp(0xBC, dst, src, w) }
func (a *Asm) Popcnt(dst, src Reg, w bool) { a.sseBitOp(0xB8, dst, src, w) }

func (a *Asm) MovReg64(dst, src Reg) {
	a.emit(a.rex(true, src >= 8, false, dst >= 8), 0x89, 0xC0|((byte(src)&7)<<3)|byte(dst&7))
}

// Xchg64 exchanges the contents of two 64-bit registers (xchg r/m64, r64).
func (a *Asm) Xchg64(x, y Reg) {
	a.emit(a.rex(true, x >= 8, false, y >= 8), 0x87, 0xC0|((byte(x)&7)<<3)|byte(y&7))
}

func (a *Asm) Movsxd(dst, src Reg) {
	a.emit(a.rex(true, dst >= 8, false, src >= 8), 0x63, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

// Movsx8 sign-extends the low byte of src into dst; w selects a 64-bit dest.
// A byte source of SPL/BPL/SIL/DIL (regs 4–7) requires a mandatory REX prefix to
// select the low-byte encoding instead of the legacy AH/CH/DH/BH.
func (a *Asm) Movsx8(dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 4 {
		a.emit(a.rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xBE, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

// Movsx16 sign-extends the low word of src into dst; w selects a 64-bit dest.
func (a *Asm) Movsx16(dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(a.rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xBF, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

func (a *Asm) Load32(dst Reg, base Reg, disp int32)  { a.memOp(0x8B, byte(dst), base, disp, false) }
func (a *Asm) Store32(base Reg, disp int32, src Reg) { a.memOp(0x89, byte(src), base, disp, false) }
func (a *Asm) Load64(dst Reg, base Reg, disp int32)  { a.memOp(0x8B, byte(dst), base, disp, true) }
func (a *Asm) Store64(base Reg, disp int32, src Reg) { a.memOp(0x89, byte(src), base, disp, true) }

// StoreImm32Mem routes addressing through baseAddr so RSP/R12 receive their
// mandatory SIB byte; the previous direct ModRM form was malformed for them.
func (a *Asm) StoreImm32Mem(base Reg, disp int32, v int32) {
	rb := base >= 8
	if rb {
		a.emit(a.rex(false, false, false, rb))
	}
	a.emit(0xC7)
	a.baseAddr(0, base, disp)
	a.imm32(v)
}

// StoreImmIdx stores an immediate directly to [base+index+disp] — `mov r/m,
// imm` (C6 /0 for a byte, C7 /0 for 16/32-bit; the reg field is the /0 digit,
// not a register). size selects the store width: 1, 2, or 4 bytes. An 8-byte
// (i64) store is NOT a single form here — `mov r/m64, imm32` sign-extends the
// immediate, which is wrong for an arbitrary 64-bit pattern; callers emit two
// 4-byte immediate stores (low32 at disp, high32 at disp+4) instead.
func (a *Asm) StoreImmIdx(base, index Reg, disp int32, imm int32, size int) {
	if size == 2 {
		a.emit(0x66) // operand-size prefix for 16-bit
	}
	if index >= 8 || base >= 8 {
		a.emit(a.rex(false, false, index >= 8, base >= 8))
	}
	op := byte(0xC7)
	if size == 1 {
		op = 0xC6
	}
	a.emit(op)
	a.sibAddr(RAX, base, index, disp) // reg field = 0 (the /0 digit)
	switch size {
	case 1:
		a.emit(byte(imm))
	case 2:
		a.emit(byte(imm), byte(imm>>8))
	default:
		a.imm32(imm)
	}
}

func (a *Asm) alu(opcode byte, dst, src Reg, w bool) {
	if w || src >= 8 || dst >= 8 {
		a.emit(a.rex(w, src >= 8, false, dst >= 8))
	}
	a.emit(opcode)
	a.emit(0xC0 | ((byte(src) & 7) << 3) | byte(dst&7))
}

func (a *Asm) Add32(dst, src Reg) { a.alu(0x01, dst, src, false) }
func (a *Asm) Sub32(dst, src Reg) { a.alu(0x29, dst, src, false) }
func (a *Asm) And32(dst, src Reg) { a.alu(0x21, dst, src, false) }
func (a *Asm) Or32(dst, src Reg)  { a.alu(0x09, dst, src, false) }
func (a *Asm) Xor32(dst, src Reg) { a.alu(0x31, dst, src, false) }
func (a *Asm) Cmp32(dst, src Reg) { a.alu(0x39, dst, src, false) }

// Inc/Dec emit the compact r/m register forms. They preserve the arithmetic
// result and every status flag except CF; callers must prove CF dead.
func (a *Asm) Inc(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0xFF, 0xC0|byte(r&7))
}

func (a *Asm) Dec(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0xFF, 0xC8|byte(r&7))
}

func (a *Asm) IMul(dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(a.rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xAF)
	a.emit(0xC0 | ((byte(dst) & 7) << 3) | byte(src&7))
}

func (a *Asm) shiftCL(digit byte, r Reg, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0xD3)
	a.emit(0xC0 | (digit << 3) | byte(r&7))
}

func (a *Asm) TestSelf(r Reg, w bool) {
	a.TestReg(r, r, w)
}

// TestReg emits TEST dst,src. Unlike AND it only updates flags, which lets the
// compiler fuse packed-word mask predicates without materializing the masked
// value first.
func (a *Asm) TestReg(dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(a.rex(w, src >= 8, false, dst >= 8))
	}
	a.emit(0x85)
	a.emit(0xC0 | ((byte(src) & 7) << 3) | byte(dst&7))
}

// TestImm emits TEST r,imm32. In the 64-bit form the immediate is sign-extended,
// matching the architectural encoding; callers must materialize wider masks.
func (a *Asm) TestImm(r Reg, imm uint32, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	if a.CompactAccumulatorImmediates && r == RAX {
		if a.EncodingStats != nil {
			a.EncodingStats.TestImm32Acc++
		}
		a.emit(0xA9)
		a.imm32(int32(imm))
		return
	}
	a.emit(0xF7)
	a.emit(0xC0 | byte(r&7)) // /0
	a.imm32(int32(imm))
}

// BtImm copies the selected register bit into CF. Unlike TEST r,imm32, its
// immediate is a bit index rather than a sign-extended mask.
func (a *Asm) BtImm(r Reg, bit uint8, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0x0f, 0xba)
	a.emit(0xe0 | byte(r&7)) // /4
	a.emit(bit)
}

type Cond byte

const (
	CondE  Cond = 0x4 // ==
	CondNE Cond = 0x5
	CondB  Cond = 0x2 // unsigned <
	CondAE Cond = 0x3
	CondBE Cond = 0x6
	CondA  Cond = 0x7
	CondL  Cond = 0xC // signed <
	CondGE Cond = 0xD
	CondLE Cond = 0xE
	CondG  Cond = 0xF
	CondP  Cond = 0xA // parity (ucomis unordered: PF=1)
	CondNP Cond = 0xB // not parity (ordered: PF=0)
	CondS  Cond = 0x8 // sign flag set (negative / top bit set)
)

func (a *Asm) SetccAL(c Cond) {
	a.emit(0x0F, 0x90|byte(c), 0xC0) // setcc al (ModRM 11 000 000)
	a.emit(0x0F, 0xB6, 0xC0)         // movzx eax, al
}

func (a *Asm) Leave() { a.emit(0xC9) }
func (a *Asm) Ret()   { a.emit(0xC3) }

func (a *Asm) Prologue() { a.emit(0x55, a.rexPrefix(0x48), 0x89, 0xE5) } // push rbp; mov rbp,rsp

func (a *Asm) SubRsp(v int32) { a.emit(a.rexPrefix(0x48), 0x81, 0xEC); a.imm32(v) }

func (a *Asm) AluRR(rrOpcode byte, dst, src Reg, w bool) { a.alu(rrOpcode, dst, src, w) }

// AluRR8 emits an 8-bit register ALU operation in r/m,reg form. A REX prefix is
// required not only for extended registers but also for SPL/BPL/SIL/DIL.
func (a *Asm) AluRR8(rrOpcode byte, dst, src Reg) {
	if dst >= 4 || src >= 4 {
		a.emit(a.rex(false, src >= 8, false, dst >= 8))
	}
	a.emit(rrOpcode, 0xC0|((byte(src)&7)<<3)|byte(dst&7))
}

func (a *Asm) AluRM(rmOpcode byte, dst, base Reg, disp int32, w bool) {
	a.memOp(rmOpcode, byte(dst), base, disp, w)
}

// AluIdx emits `dst = dst <op> [base + index + disp]` (reg,r/m form) — folding a
// bounds-checked memory operand into an ALU op. rmOpcode is the reg,r/m opcode.
func (a *Asm) AluIdx(rmOpcode byte, dst, base, index Reg, disp int32, w bool) {
	if w || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(a.rex(w, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(rmOpcode)
	a.sibAddr(dst, base, index, disp)
}

// ImulIdx emits `dst = dst * [base + index + disp]`.
func (a *Asm) ImulIdx(dst, base, index Reg, disp int32, w bool) {
	if w || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(a.rex(w, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(0x0F, 0xAF)
	a.sibAddr(dst, base, index, disp)
}

// digit selects add/or/and/sub/xor/cmp.
func (a *Asm) AluRI(digit byte, dst Reg, imm int32, w bool) {
	if w || dst >= 8 {
		a.emit(a.rex(w, false, false, dst >= 8))
	}
	if imm >= -128 && imm <= 127 {
		a.emit(0x83, 0xC0|(digit<<3)|byte(dst&7), byte(imm))
	} else {
		if a.CompactAccumulatorImmediates && dst == RAX {
			if a.EncodingStats != nil {
				a.EncodingStats.AluImm32Acc++
			}
			a.emit(0x05 + digit<<3)
			a.imm32(imm)
			return
		}
		a.emit(0x81, 0xC0|(digit<<3)|byte(dst&7))
		a.imm32(imm)
	}
}

func (a *Asm) ImulRM(dst, base Reg, disp int32, w bool) {
	if w || dst >= 8 || base >= 8 {
		a.emit(a.rex(w, dst >= 8, false, base >= 8))
	}
	a.emit(0x0F, 0xAF)
	a.baseAddr(byte(dst), base, disp)
}

func (a *Asm) ImulRI(dst Reg, imm int32, w bool) {
	if w || dst >= 8 {
		a.emit(a.rex(w, dst >= 8, false, dst >= 8))
	}
	mod := byte(0xC0) | ((byte(dst) & 7) << 3) | byte(dst&7)
	if imm >= -128 && imm <= 127 {
		a.emit(0x6B, mod, byte(imm))
	} else {
		a.emit(0x69, mod)
		a.imm32(imm)
	}
}

// ImulRRI is the three-operand IMUL: dst = src * imm (src a distinct register),
// avoiding a preceding mov dst,src that the two-operand ImulRI would require.
func (a *Asm) ImulRRI(dst, src Reg, imm int32, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(a.rex(w, dst >= 8, false, src >= 8))
	}
	mod := byte(0xC0) | ((byte(dst) & 7) << 3) | byte(src&7)
	if imm >= -128 && imm <= 127 {
		a.emit(0x6B, mod, byte(imm))
	} else {
		a.emit(0x69, mod)
		a.imm32(imm)
	}
}

func (a *Asm) ShiftImm(digit byte, dst Reg, count byte, w bool) {
	if count == 0 {
		if a.EncodingStats != nil {
			a.EncodingStats.ShiftImmZero++
			a.EncodingStats.ShiftSaved += 3
			if w || dst >= 8 {
				a.EncodingStats.ShiftSaved++
			}
		}
		return
	}
	if w || dst >= 8 {
		a.emit(a.rex(w, false, false, dst >= 8))
	}
	mod := byte(0xC0) | (digit << 3) | byte(dst&7)
	if count == 1 {
		if a.EncodingStats != nil {
			a.EncodingStats.ShiftImmOne++
			a.EncodingStats.ShiftSaved++
		}
		a.emit(0xD1, mod)
		return
	}
	if a.EncodingStats != nil {
		a.EncodingStats.ShiftImm8++
	}
	a.emit(0xC1, mod, count)
}

func (a *Asm) ShiftCL(digit byte, dst Reg, w bool) { a.shiftCL(digit, dst, w) }

func (a *Asm) MovImm64(r Reg, v uint64) {
	// A 32-bit destination write zeroes the upper half, making B8+rd imm32
	// exactly equivalent for every zero-extended 32-bit value. C7 /0 with REX.W
	// sign-extends imm32 and covers the complementary signed range. Keep movabs
	// only for values that need all eight immediate bytes.
	if uint64(uint32(v)) == v {
		if a.EncodingStats != nil {
			a.EncodingStats.MovImmNarrow++
			a.EncodingStats.MovImmSaved += 5
			if r >= 8 {
				a.EncodingStats.MovImmSaved--
			}
		}
		a.MovImm32(r, int32(v))
		return
	}
	if uint64(int64(int32(v))) == v {
		if a.EncodingStats != nil {
			a.EncodingStats.MovImm32Sext++
			a.EncodingStats.MovImmNarrow++
			a.EncodingStats.MovImmSaved += 3
		}
		a.emit(a.rex(true, false, false, r >= 8), 0xC7, 0xC0|byte(r&7))
		a.imm32(int32(v))
		return
	}
	if a.EncodingStats != nil {
		a.EncodingStats.MovImm64++
	}
	a.emit(a.rex(true, false, false, r >= 8), 0xB8|byte(r&7))
	var t [8]byte
	t[0] = byte(v)
	t[1] = byte(v >> 8)
	t[2] = byte(v >> 16)
	t[3] = byte(v >> 24)
	t[4] = byte(v >> 32)
	t[5] = byte(v >> 40)
	t[6] = byte(v >> 48)
	t[7] = byte(v >> 56)
	a.B = append(a.B, t[:]...)
}

func (a *Asm) AddRsp(v int32) { a.emit(a.rexPrefix(0x48), 0x81, 0xC4); a.imm32(v) }

func (a *Asm) rspMem(opcode byte, reg byte, disp int32, w bool) {
	rr := reg >= 8
	if w || rr {
		a.emit(a.rex(w, rr, false, false))
	}
	a.emit(opcode)
	a.baseAddr(reg, RSP, disp)
}

func (a *Asm) StoreRsp32(disp int32, src Reg) { a.rspMem(0x89, byte(src), disp, false) }
func (a *Asm) LoadRsp32(dst Reg, disp int32)  { a.rspMem(0x8B, byte(dst), disp, false) }
func (a *Asm) StoreRsp64(disp int32, src Reg) { a.rspMem(0x89, byte(src), disp, true) }
func (a *Asm) LoadRsp64(dst Reg, disp int32)  { a.rspMem(0x8B, byte(dst), disp, true) }

func (a *Asm) LeaRsp(dst Reg, disp int32) { a.rspMem(0x8D, byte(dst), disp, true) }

func (a *Asm) MovFromRsp(dst Reg) {
	a.emit(a.rex(true, false, false, dst >= 8), 0x89, 0xC0|(4<<3)|byte(dst&7))
}

func (a *Asm) CallRel32() int { a.emit(0xE8); off := a.Len(); a.imm32(0); return off }

// CallMem emits CALL qword [base+disp] (FF /2) — an indirect call through a
// memory-resident code pointer, leaving all registers free for arguments.
func (a *Asm) CallMem(base Reg, disp int32) { a.memOp(0xFF, 2, base, disp, false) }

func (a *Asm) CallReg(r Reg) {
	if r >= 8 {
		a.emit(a.rexPrefix(0x41))
	}
	a.emit(0xFF, 0xD0|byte(r&7))
}

func (a *Asm) LeaScaled(dst, base, index Reg, scaleLog uint8, disp int32) {
	a.LeaScaledW(dst, base, index, scaleLog, disp, true)
}

// LeaScaledW is LeaScaled with an explicit destination width. w=false yields a
// 32-bit result (the address is computed in 64-bit and truncated+zero-extended),
// which matches i32 wraparound arithmetic.
func (a *Asm) LeaScaledW(dst, base, index Reg, scaleLog uint8, disp int32, w bool) {
	if w || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(a.rex(w, dst >= 8, index >= 8, base >= 8))
	}
	mod := addrMode(base, disp)
	a.recordAddress(base, mod)
	a.emit(0x8D, mod|((byte(dst)&7)<<3)|0x04)
	a.emit((scaleLog << 6) | ((byte(index) & 7) << 3) | byte(base&7))
	a.emitDisp(mod, disp)
}

// LeaDispW is `lea dst, [base + disp]` with an explicit destination width.
func (a *Asm) LeaDispW(dst, base Reg, disp int32, w bool) { a.memOp(0x8D, byte(dst), base, disp, w) }

func (a *Asm) Add64(dst, src Reg) {
	a.emit(a.rex(true, src >= 8, false, dst >= 8), 0x01, 0xC0|((byte(src)&7)<<3)|byte(dst&7))
}

func (a *Asm) Cmp64(x, y Reg) {
	a.emit(a.rex(true, y >= 8, false, x >= 8), 0x39, 0xC0|((byte(y)&7)<<3)|byte(x&7))
}

func (a *Asm) LeaDisp(dst, base Reg, disp int32) { a.memOp(0x8D, byte(dst), base, disp, true) }

// String ops for bulk memory. In 64-bit mode these use RSI/RDI (64-bit
// pointers) and RCX (64-bit count); rep stosb stores AL. Direction is DF.
func (a *Asm) RepMovsb() { a.emit(0xF3, 0xA4) } // rep movs byte [RDI] <- [RSI], RCX times
func (a *Asm) RepStosb() { a.emit(0xF3, 0xAA) } // rep stos byte [RDI] <- AL, RCX times
func (a *Asm) Std()      { a.emit(0xFD) }       // set direction flag (decrement)
func (a *Asm) Cld()      { a.emit(0xFC) }       // clear direction flag (increment)

// LoadIdx loads `size` bytes from [base+index] into dst. signed selects sign-
// vs zero-extension; wide selects a 64-bit destination (i64), so signed
// sub-width loads sign-extend to all 64 bits instead of only 32. Unsigned loads
// zero-extend to 64 regardless of wide (x86 movzx/32-bit mov clear the top).
// sibAddr emits ModRM + SIB and the shortest displacement for a
// [base + index + disp] operand (scale 1) with the given reg field.
func (a *Asm) sibAddr(reg, base, index Reg, disp int32) {
	mod := addrMode(base, disp)
	a.recordAddress(base, mod)
	a.emit(mod | ((byte(reg) & 7) << 3) | 0x04)     // ModRM rm=100 (SIB)
	a.emit(((byte(index) & 7) << 3) | byte(base&7)) // SIB scale=0 index base
	a.emitDisp(mod, disp)
}

func (a *Asm) LoadIdx(dst, base, index Reg, disp int32, size int, signed, wide bool) {
	var op []byte
	rexW := false
	switch {
	case size == 8:
		op, rexW = []byte{0x8B}, true // mov r64, m64
	case size == 4 && signed && wide:
		op, rexW = []byte{0x63}, true // movsxd r64, m32
	case size == 4:
		op = []byte{0x8B} // mov r32, m32 (zero-extends to 64)
	case size == 1 && signed:
		op, rexW = []byte{0x0F, 0xBE}, wide // movsx r, m8
	case size == 1:
		op = []byte{0x0F, 0xB6} // movzx r, m8 (zero-extends to 64)
	case size == 2 && signed:
		op, rexW = []byte{0x0F, 0xBF}, wide // movsx r, m16
	default: // size == 2 unsigned
		op = []byte{0x0F, 0xB7} // movzx r, m16 (zero-extends to 64)
	}
	if rexW || dst >= 8 || index >= 8 || base >= 8 {
		a.emit(a.rex(rexW, dst >= 8, index >= 8, base >= 8))
	}
	a.emit(op...)
	a.sibAddr(dst, base, index, disp)
}

func (a *Asm) StoreIdx(base, index, src Reg, disp int32, size int) {
	if size == 2 {
		a.emit(0x66) // operand-size prefix for 16-bit
	}
	w := size == 8
	// A byte store from SPL/BPL/SIL/DIL (regs 4–7) needs a mandatory REX to select
	// the low-byte encoding instead of the legacy AH/CH/DH/BH.
	if w || src >= 8 || index >= 8 || base >= 8 || (size == 1 && src >= 4) {
		a.emit(a.rex(w, src >= 8, index >= 8, base >= 8))
	}
	op := byte(0x89)
	if size == 1 {
		op = 0x88
	}
	a.emit(op)
	a.sibAddr(src, base, index, disp)
}

// LockXaddIdx32 atomically adds src to the 32-bit memory operand and replaces
// src with the operand's previous value. The LOCK prefix supplies the Wasm
// threads proposal's sequentially consistent ordering on x86-64.
func (a *Asm) LockXaddIdx32(base, index, src Reg, disp int32) {
	a.emit(0xF0)
	if src >= 8 || index >= 8 || base >= 8 {
		a.emit(a.rex(false, src >= 8, index >= 8, base >= 8))
	}
	a.emit(0x0F, 0xC1)
	a.sibAddr(src, base, index, disp)
}

func (a *Asm) LockXaddIdx(base, index, src Reg, disp int32, size int) {
	if size == 2 {
		a.emit(0x66)
	}
	a.emit(0xF0)
	w := size == 8
	if w || src >= 8 || index >= 8 || base >= 8 || (size == 1 && src >= 4) {
		a.emit(a.rex(w, src >= 8, index >= 8, base >= 8))
	}
	op := byte(0xC1)
	if size == 1 {
		op = 0xC0
	}
	a.emit(0x0F, op)
	a.sibAddr(src, base, index, disp)
}

func (a *Asm) Movzx8(dst, src Reg, wide bool) {
	if wide || dst >= 8 || src >= 4 {
		a.emit(a.rex(wide, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xB6, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

func (a *Asm) Movzx16(dst, src Reg, wide bool) {
	if wide || dst >= 8 || src >= 8 {
		a.emit(a.rex(wide, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0xB7, 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

// XchgIdx atomically exchanges src with a memory operand. Memory-form XCHG is
// implicitly locked and is the x86-64 sequentially consistent atomic-store
// primitive; src receives the discarded old value.
func (a *Asm) XchgIdx(base, index, src Reg, disp int32, size int) {
	if size == 2 {
		a.emit(0x66)
	}
	w := size == 8
	if w || src >= 8 || index >= 8 || base >= 8 || (size == 1 && src >= 4) {
		a.emit(a.rex(w, src >= 8, index >= 8, base >= 8))
	}
	op := byte(0x87)
	if size == 1 {
		op = 0x86
	}
	a.emit(op)
	a.sibAddr(src, base, index, disp)
}

func (a *Asm) Mfence() { a.emit(0x0F, 0xAE, 0xF0) }

// LockCmpxchgIdx compares the memory operand with RAX/EAX/AX/AL and, on match,
// stores src. On failure it loads the observed memory value into the matching
// accumulator width and clears ZF.
func (a *Asm) LockCmpxchgIdx(base, index, src Reg, disp int32, size int) {
	if size == 2 {
		a.emit(0x66)
	}
	a.emit(0xF0)
	w := size == 8
	if w || src >= 8 || index >= 8 || base >= 8 || (size == 1 && src >= 4) {
		a.emit(a.rex(w, src >= 8, index >= 8, base >= 8))
	}
	op := byte(0xB1)
	if size == 1 {
		op = 0xB0
	}
	a.emit(0x0F, op)
	a.sibAddr(src, base, index, disp)
}

func (a *Asm) Cdq(w bool) {
	if w {
		a.emit(a.rexPrefix(0x48))
	}
	a.emit(0x99)
}

func (a *Asm) Idiv(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0xF7, 0xF8|byte(r&7)) // 0xF7 /7
}

func (a *Asm) Div(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0xF7, 0xF0|byte(r&7)) // 0xF7 /6
}

// Mul computes RDX:RAX = RAX * r (unsigned); the high half lands in RDX. Used by
// magic division to take the high half of a widening multiply.
func (a *Asm) Mul(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0xF7, 0xE0|byte(r&7)) // 0xF7 /4
}

// IMulHigh computes RDX:RAX = RAX * r (signed); the high half lands in RDX.
func (a *Asm) IMulHigh(r Reg, w bool) {
	if w || r >= 8 {
		a.emit(a.rex(w, false, false, r >= 8))
	}
	a.emit(0xF7, 0xE8|byte(r&7)) // 0xF7 /5
}

// XorSelf32 zeroes r and clears the upper 32 bits.
func (a *Asm) XorSelf32(r Reg) { a.alu(0x31, r, r, false) }

func (a *Asm) JmpPlaceholder() int { a.emit(0xE9); off := a.Len(); a.imm32(0); return off }

func (a *Asm) JccPlaceholder(c Cond) int {
	a.emit(0x0F, 0x80|byte(c))
	off := a.Len()
	a.imm32(0)
	return off
}

// JcxzPlaceholder emits JECXZ (wide=false) or JRCXZ (wide=true) with an
// unresolved rel8 target and returns the displacement-byte offset.
func (a *Asm) JcxzPlaceholder(wide bool) int {
	if !wide {
		a.emit(0x67) // address-size override selects ECX in 64-bit mode
	}
	a.emit(0xE3, 0)
	return a.Len() - 1
}

func (a *Asm) PatchRel8(at, target int) bool {
	delta := target - (at + 1)
	if delta < -128 || delta > 127 {
		return false
	}
	a.B[at] = byte(int8(delta))
	return true
}

func (a *Asm) JccRel8(c Cond, target int) bool {
	delta := target - (a.Len() + 2)
	if delta < -128 || delta > 127 {
		return false
	}
	a.emit(0x70|byte(c), byte(int8(delta)))
	return true
}

func (a *Asm) JmpRel8Placeholder() int {
	a.emit(0xEB, 0)
	return a.Len() - 1
}

func (a *Asm) PatchRel32(at, target int) {
	a.recordRel32(at, target)
	a.PatchU32(at, uint32(int32(target-(at+4))))
}

func (a *Asm) JmpBack(target int) {
	a.emit(0xE9)
	off := a.Len()
	a.recordRel32(off, target)
	a.imm32(int32(target - (off + 4)))
}

func (a *Asm) recordRel32(at, target int) {
	a.Rel32Count++
	if a.Rel32SiteLimit == 0 {
		return
	}
	if len(a.Rel32Sites) >= a.Rel32SiteLimit {
		a.Rel32Overflow = true
		return
	}
	// Packing the site offset and finalizer flags into four bytes bounds the
	// symbolic path to 512 MiB functions. Larger functions retain their emitted
	// near forms through Rel32Overflow rather than allocating wider records.
	if at < 0 || target < 0 || uint64(at) > uint64(rel32OffsetMask) || uint64(target) > uint64(rel32OffsetMask) {
		a.Rel32Overflow = true
		return
	}
	kind := Rel32Other
	if at >= 1 && a.B[at-1] == 0xe9 {
		kind = Rel32Jmp
	} else if at >= 2 && a.B[at-2] == 0x0f && a.B[at-1]&0xf0 == 0x80 {
		kind = Rel32Jcc
	}
	a.Rel32Sites = append(a.Rel32Sites, Rel32Site{atAndFlags: uint32(at) | uint32(kind)<<rel32KindShift})
}

// ForgetRel32 removes the retained record for a displacement field eliminated
// by a later rewrite. The underlying scratch is retained for the next function.
func (a *Asm) ForgetRel32(at int) {
	for i := len(a.Rel32Sites) - 1; i >= 0; i-- {
		if a.Rel32Sites[i].At() == at {
			copy(a.Rel32Sites[i:], a.Rel32Sites[i+1:])
			a.Rel32Sites = a.Rel32Sites[:len(a.Rel32Sites)-1]
			return
		}
	}
}

// KeepRel32Long retains the recorded displacement for remapping but prevents
// branch relaxation. This is required when surrounding data addresses a fixed
// width instruction vector by byte stride.
func (a *Asm) KeepRel32Long(at int) {
	for i := len(a.Rel32Sites) - 1; i >= 0; i-- {
		if a.Rel32Sites[i].At() == at {
			a.Rel32Sites[i].atAndFlags &^= uint32(3) << rel32KindShift
			return
		}
	}
}

func (a *Asm) Cmovcc(cc Cond, dst, src Reg, w bool) {
	if w || dst >= 8 || src >= 8 {
		a.emit(a.rex(w, dst >= 8, false, src >= 8))
	}
	a.emit(0x0F, 0x40|byte(cc), 0xC0|((byte(dst)&7)<<3)|byte(src&7))
}

func (a *Asm) SetccReg(c Cond, dst Reg) {
	a.SetccReg8(c, dst)
	if dst >= 4 {
		a.emit(a.rex(false, dst >= 8, false, dst >= 8))
	}
	a.emit(0x0F, 0xB6, 0xC0|((byte(dst)&7)<<3)|byte(dst&7))
}

// SetccReg8 writes only dst's low byte. Callers may use it when the next sink
// observes exactly that byte; the rest of dst remains unspecified.
func (a *Asm) SetccReg8(c Cond, dst Reg) {
	if dst >= 4 {
		a.emit(a.rex(false, false, false, dst >= 8))
	}
	a.emit(0x0F, 0x90|byte(c), 0xC0|byte(dst&7))
}

// nopSeqs[n] is the canonical n-byte NOP (Intel SDM recommended multi-byte
// forms), used for code alignment padding.
var nopSeqs = [10][]byte{
	1: {0x90},
	2: {0x66, 0x90},
	3: {0x0F, 0x1F, 0x00},
	4: {0x0F, 0x1F, 0x40, 0x00},
	5: {0x0F, 0x1F, 0x44, 0x00, 0x00},
	6: {0x66, 0x0F, 0x1F, 0x44, 0x00, 0x00},
	7: {0x0F, 0x1F, 0x80, 0x00, 0x00, 0x00, 0x00},
	8: {0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00},
	9: {0x66, 0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00},
}

// Align16 pads with multi-byte NOPs so the next emitted instruction starts on a
// 16-byte boundary. Used for loop-top alignment: the pad sits on the entry path
// (before the backward-branch target), so it executes once per loop entry, never
// per iteration.
func (a *Asm) Align16() {
	pad := (16 - len(a.B)%16) % 16
	a.nop(pad)
}

// AlignLoop normally preserves compact 16-byte loop alignment. If the entry
// path has already reached the last eight bytes of a 32-byte fetch block, it
// advances to offset eight in the next block; this leaves room for the header
// compare/branch without placing the first wide ALU instruction on a boundary.
func (a *Asm) AlignLoop() {
	offset := len(a.B) % 32
	pad := (16 - len(a.B)%16) % 16
	if offset >= 24 {
		pad = 32 - offset + 8
	}
	a.nop(pad)
}

func (a *Asm) nop(pad int) {
	for pad > 0 {
		n := pad
		if n > 9 {
			n = 9
		}
		a.B = append(a.B, nopSeqs[n]...)
		pad -= n
	}
}

// JmpReg emits JMP r64 (FF /4) — an indirect jump for jump-table dispatch.
func (a *Asm) JmpReg(r Reg) {
	if r >= 8 {
		a.emit(a.rexPrefix(0x41))
	}
	a.emit(0xFF, 0xE0|byte(r&7))
}

// LeaRipPlaceholder emits `lea dst, [rip+disp32]` with a zero displacement and
// returns the displacement's byte offset for PatchRel32 (the displacement is
// relative to the end of the instruction, exactly like a jump's rel32).
func (a *Asm) LeaRipPlaceholder(dst Reg) int {
	rex := byte(0x48)
	if dst >= 8 {
		rex |= 0x04 // REX.R
	}
	a.emit(a.rexPrefix(rex), 0x8D, byte(dst&7)<<3|0x05) // ModRM mod=00 rm=101 → RIP-relative
	a.recordRipAddress()
	off := a.Len()
	a.imm32(0)
	return off
}

// Neg emits NEG r (F7 /3): two's-complement negation.
func (a *Asm) Neg(r Reg, w bool) {
	rex := byte(0x40)
	if w {
		rex |= 0x08
	}
	if r >= 8 {
		rex |= 0x01
	}
	if rex != 0x40 || w {
		a.emit(a.rexPrefix(rex))
	}
	a.emit(0xF7, 0xD8|byte(r&7))
}
