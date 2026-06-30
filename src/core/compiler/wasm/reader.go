package wasm

import (
	"encoding/binary"
	"unicode/utf8"
)

type reader struct {
	data []byte
	pos  int
}

func newReader(data []byte) *reader { return &reader{data: data} }
func (r *reader) off() int          { return r.pos }
func (r *reader) left() int         { return len(r.data) - r.pos }
func (r *reader) has() bool         { return r.left() > 0 }
func (r *reader) peek() (byte, bool) {
	if r.pos >= len(r.data) {
		return 0, false
	}
	return r.data[r.pos], true
}
func (r *reader) byte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.pos}
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}
func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.pos}
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}
func (r *reader) le32() (uint32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}
func (r *reader) le64() (uint64, error) {
	b, err := r.bytes(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

func (r *reader) leb(signed bool, maxBits uint32) (uint64, error) {
	var result uint64
	var shift uint32
	for i := 0; ; i++ {
		if i >= int((maxBits+6)/7) {
			return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
		}
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		if shift >= 64 && (b&0x7f) != 0 {
			return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
		}
		if shift < 64 {
			result |= uint64(b&0x7f) << shift
		}
		cont := b&0x80 != 0
		shift += 7
		if !cont {
			if shift > maxBits {
				extra := shift - maxBits
				used := 7 - extra
				mask := byte(((uint16(1) << extra) - 1) << used)
				if signed && (b&(1<<(used-1))) != 0 {
					if b&mask != mask {
						return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
					}
				} else if b&mask != 0 {
					return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
				}
			}
			if signed && shift < 64 && (b&0x40) != 0 {
				result |= ^uint64(0) << shift
			}
			return result, nil
		}
		if shift >= maxBits+7 {
			return 0, &DecodeError{Code: ErrMalformedLEB, Offset: r.pos}
		}
	}
}
func (r *reader) u32() (uint32, error) { v, err := r.leb(false, 32); return uint32(v), err }
func (r *reader) u33() (uint64, error) { return r.leb(false, 33) }
func (r *reader) u64() (uint64, error) { return r.leb(false, 64) }
func (r *reader) s33() (int64, error)  { v, err := r.leb(true, 33); return int64(v), err }
func (r *reader) i32() (int32, error)  { v, err := r.leb(true, 32); return int32(v), err }
func (r *reader) i64() (int64, error)  { v, err := r.leb(true, 64); return int64(v), err }
func (r *reader) name() (string, error) {
	n, err := r.u32()
	if err != nil {
		return "", err
	}
	start := r.off()
	b, err := r.bytes(int(n))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", &DecodeError{Code: ErrInvalidSection, Offset: start}
	}
	return string(b), nil
}

func readVec[T any](r *reader, fn func(*reader) (T, error)) ([]T, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	// The declared vector length is attacker-controlled. Do not use it as a
	// capacity hint directly: a tiny malformed payload can otherwise request a
	// multi-gigabyte allocation before the first element read discovers EOF. Keep
	// the hint in int space until n has been proven smaller, so this is safe on
	// 32-bit Go as well as amd64.
	capHint := r.left()
	if uint64(n) < uint64(capHint) {
		capHint = int(n)
	}
	out := make([]T, 0, capHint)
	for i := uint32(0); i < n; i++ {
		v, err := fn(r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func ptr[T any](v T) *T { return &v }
