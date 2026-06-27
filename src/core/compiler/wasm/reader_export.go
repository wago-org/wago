package wasm

import "encoding/binary"

// Reader is a bounds-checked cursor over encoded wasm bytecode. It is used by
// the amd64 backend after the wasm frontend has decoded, validated, and
// re-serialized the supported instruction subset for single-pass codegen.
type Reader struct {
	data []byte
	pos  int
}

func NewReader(bytecode []byte) *Reader { return &Reader{data: bytecode} }

func (r *Reader) Offset() int    { return r.pos }
func (r *Reader) BytesLeft() int { return len(r.data) - r.pos }
func (r *Reader) HasNext() bool  { return r.BytesLeft() > 0 }

// JumpTo moves the cursor to an absolute position in [0, len].
func (r *Reader) JumpTo(pos int) error {
	if pos < 0 || pos > len(r.data) {
		return &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.pos}
	}
	r.pos = pos
	return nil
}

func (r *Reader) Step(count int) error { return r.JumpTo(r.pos + count) }

func (r *Reader) Byte() (byte, error) {
	old := r.pos
	if err := r.Step(1); err != nil {
		return 0, err
	}
	return r.data[old], nil
}

func (r *Reader) LEU32() (uint32, error) {
	old := r.pos
	if err := r.Step(4); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(r.data[old:]), nil
}

func (r *Reader) LEU64() (uint64, error) {
	old := r.pos
	if err := r.Step(8); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(r.data[old:]), nil
}

func (r *Reader) leb128(signedInt bool, maxBits uint32) (uint64, error) {
	var result uint64
	var bitsWritten uint32
	var b byte = 0xFF
	for uint32(b)&0x80 != 0 {
		var err error
		b, err = r.Byte()
		if err != nil {
			return 0, err
		}
		if bitsWritten >= maxBits {
			return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
		}
		lowByte := uint32(b) & 0x7F
		result |= uint64(lowByte) << uint64(bitsWritten)
		bitsWritten += 7
		if bitsWritten > maxBits {
			over := bitsWritten - maxBits
			bitMask := (uint32(0xFF) << ((6 - over) + 1)) & 0x7F
			if signedInt && (uint32(b)&(1<<(6-over))) != 0 {
				if uint32(b)&bitMask != bitMask {
					return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
				}
			} else if uint32(b)&bitMask != 0 {
				return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
			}
		}
	}
	if signedInt && (uint32(b)&0x40) != 0 && bitsWritten < 64 {
		result |= ^uint64(0) << bitsWritten
	}
	return result, nil
}

func (r *Reader) U32() (uint32, error) { v, err := r.leb128(false, 32); return uint32(v), err }
func (r *Reader) U64() (uint64, error) { return r.leb128(false, 64) }
func (r *Reader) I32() (int32, error)  { v, err := r.leb128(true, 32); return int32(uint32(v)), err }
func (r *Reader) I64() (int64, error)  { v, err := r.leb128(true, 64); return int64(v), err }

func (r *Reader) Peek() (byte, bool) {
	if r.pos >= len(r.data) {
		return 0, false
	}
	return r.data[r.pos], true
}

func (r *Reader) Bytes(n int) ([]byte, error) {
	old := r.pos
	if err := r.Step(n); err != nil {
		return nil, err
	}
	return r.data[old:r.pos], nil
}
