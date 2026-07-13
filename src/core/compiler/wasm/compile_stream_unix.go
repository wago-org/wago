//go:build unix

package wasm

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unicode/utf8"
)

// CompileStreamModule is a compile-oriented decoded module whose code and data
// section payloads may be backed by short-lived private mappings. Close must be
// called after validation/lowering has copied the reusable product bytes.
type CompileStreamModule struct {
	Module *Module
	maps   [][]byte
}

func (d *CompileStreamModule) Close() {
	if d == nil {
		return
	}
	for _, b := range d.maps {
		_ = syscall.Munmap(b)
	}
	d.maps = nil
}

// DecodeModuleForCompileStream decodes a wasm stream one section at a time.
// Unknown custom payloads are drained through a fixed window; code and data
// payloads are spooled to private mappings so they never require one whole
// source-buffer allocation. Structured metadata retains the same direct decoder
// and strict acceptance rules as DecodeModuleForCompile.
func DecodeModuleForCompileStream(src io.Reader) (*CompileStreamModule, error) {
	s := &compileStreamReader{r: src}
	header, err := s.bytes(8)
	if err != nil {
		return nil, err
	}
	if string(header[:4]) != "\x00asm" {
		return nil, &DecodeError{Code: ErrBadMagic, Offset: 0}
	}
	if header[4] != 1 || header[5] != 0 || header[6] != 0 || header[7] != 0 {
		return nil, &DecodeError{Code: ErrBadVersion, Offset: 4}
	}
	dm := &directModule{}
	out := &CompileStreamModule{Module: &dm.m}
	fail := func(err error) (*CompileStreamModule, error) {
		out.Close()
		return nil, err
	}
	lastOrder := 0
	seen := map[byte]bool{}
	for {
		id, ok, err := s.optionalByte()
		if err != nil {
			return fail(err)
		}
		if !ok {
			break
		}
		size, err := s.u32()
		if err != nil {
			return fail(err)
		}
		start := s.off
		end64 := int64(start) + int64(size)
		if end64 > int64(maxStreamInt) {
			return fail(&DecodeError{Code: ErrIndexOutOfBounds, Offset: start})
		}
		end := int(end64)
		if id != secCustom {
			ord, valid := sectionOrder[id]
			if !valid {
				return fail(sectionStreamError(&DecodeError{Code: ErrInvalidSection, Offset: start - 1}, id, start, end))
			}
			if ord < lastOrder {
				return fail(sectionStreamError(&DecodeError{Code: ErrSectionOrder, Offset: start - 1}, id, start, end))
			}
			if seen[id] {
				return fail(sectionStreamError(&DecodeError{Code: ErrDuplicateSection, Offset: start - 1}, id, start, end))
			}
			seen[id] = true
			lastOrder = ord
		}

		var decodeErr error
		switch id {
		case secCustom:
			decodeErr = decodeDirectCustomStream(dm, s, int(size))
		case secCode:
			payload, release, err := s.mappedPayload(int(size))
			if err != nil {
				decodeErr = err
				break
			}
			var sub reader
			sub.reset(payload)
			dm.m.Code, dm.usesDataCountInstr, decodeErr = decodeDirectCodeSection(&sub, moduleMemargOffset64(&dm.m))
			if decodeErr == nil && sub.has() {
				decodeErr = &DecodeError{Code: ErrSectionSizeMismatch, Offset: sub.off()}
			}
			if decodeErr != nil {
				release()
			} else {
				out.maps = append(out.maps, payload)
			}
		case secData:
			payload, release, err := s.mappedPayload(int(size))
			if err != nil {
				decodeErr = err
				break
			}
			var sub reader
			sub.reset(payload)
			decodeErr = decodeDirectDataSection(dm, &sub)
			if decodeErr == nil && sub.has() {
				decodeErr = &DecodeError{Code: ErrSectionSizeMismatch, Offset: sub.off()}
			}
			if decodeErr != nil {
				release()
			} else {
				out.maps = append(out.maps, payload)
			}
		default:
			payload, release, err := s.mappedPayload(int(size))
			if err != nil {
				decodeErr = err
				break
			}
			var sub reader
			sub.reset(payload)
			switch id {
			case secTable:
				decodeErr = decodeDirectTableSection(dm, &sub)
			case secGlobal:
				decodeErr = decodeDirectGlobalSection(dm, &sub)
			case secElement:
				decodeErr = decodeDirectElementSection(dm, &sub)
			default:
				decodeErr = decodeSection(&dm.m, &sub, id)
			}
			if decodeErr == nil && sub.has() {
				decodeErr = &DecodeError{Code: ErrSectionSizeMismatch, Offset: sub.off()}
			}
			// Tables, globals, and elements retain direct const-expression byte
			// slices. Move those tiny structural payloads into product-owned Go
			// storage before unmapping their section; all other decoded metadata is
			// already scalar/slice-owned by the decoder.
			if decodeErr == nil {
				switch id {
				case secTable:
					cloneDirectTableExprs(&dm.direct)
				case secGlobal:
					cloneDirectGlobalExprs(&dm.direct)
				case secElement:
					cloneDirectElementExprs(&dm.direct)
				}
			}
			release()
		}
		if decodeErr != nil {
			return fail(sectionStreamError(decodeErr, id, start, end))
		}
		if s.off != end {
			return fail(sectionStreamError(&DecodeError{Code: ErrSectionSizeMismatch, Offset: s.off}, id, start, end))
		}
	}
	if len(dm.m.FuncTypes) != len(dm.m.Code) {
		return fail(&DecodeError{Code: ErrInvalidModule, Offset: s.off})
	}
	if dm.m.DataCount != nil && uint64(*dm.m.DataCount) != uint64(len(dm.m.Data)) {
		return fail(&DecodeError{Code: ErrInvalidModule, Offset: s.off})
	}
	if dm.m.DataCount == nil && dm.usesDataCountInstr {
		return fail(&DecodeError{Code: ErrInvalidModule, Offset: s.off})
	}
	dm.populateCodeBodies()
	return out, nil
}

const maxStreamInt = int(^uint(0) >> 1)

func sectionStreamError(err error, id byte, start, end int) error {
	if de, ok := err.(*DecodeError); ok {
		de.SectionID, de.SectionStart, de.SectionEnd = id, start, end
		if de.Offset == 0 {
			de.Offset = start
		}
	}
	return err
}

func decodeDirectCustomStream(dm *directModule, s *compileStreamReader, size int) error {
	begin := s.off
	nameLen, err := s.u32()
	if err != nil {
		return err
	}
	if int64(nameLen) > int64(size)-(int64(s.off)-int64(begin)) {
		return &DecodeError{Code: ErrIndexOutOfBounds, Offset: s.off}
	}
	nameStart := s.off
	isName, err := s.customNameIsName(int(nameLen), nameStart)
	if err != nil {
		return err
	}
	remaining := size - (s.off - begin)
	if remaining < 0 {
		return &DecodeError{Code: ErrIndexOutOfBounds, Offset: s.off}
	}
	if !isName {
		return s.drain(remaining)
	}
	if dm.seenName {
		return &DecodeError{Code: ErrInvalidSection, Offset: s.off}
	}
	payload, release, err := s.mappedPayload(remaining)
	if err != nil {
		return err
	}
	ns, err := decodeNameSec(payload)
	release()
	if err != nil {
		return err
	}
	dm.m.NameSec = ns
	dm.seenName = true
	return nil
}

func cloneDirectExprBytes(e directConstExpr) directConstExpr {
	e.body = append([]byte(nil), e.body...)
	return e
}

func cloneDirectTableExprs(d *directValidationEnv) {
	for i := range d.tableInits {
		d.tableInits[i] = cloneDirectExprBytes(d.tableInits[i])
	}
}

func cloneDirectGlobalExprs(d *directValidationEnv) {
	for i := range d.globalInits {
		d.globalInits[i] = cloneDirectExprBytes(d.globalInits[i])
	}
}

func cloneDirectElementExprs(d *directValidationEnv) {
	for i := range d.elements {
		d.elements[i].offset = cloneDirectExprBytes(d.elements[i].offset)
		for j := range d.elements[i].exprs {
			d.elements[i].exprs[j] = cloneDirectExprBytes(d.elements[i].exprs[j])
		}
	}
}

// customNameIsName validates a custom-section name while comparing it to the
// only structured custom section we retain. In particular, an unknown custom
// section with a producer-controlled multi-megabyte name does not allocate a
// same-sized Go string merely to decide that its payload should be drained.
func (s *compileStreamReader) customNameIsName(n, start int) (bool, error) {
	match := n == len("name")
	var seq [4]byte
	seqN, want := 0, 0
	for i := 0; i < n; i++ {
		b, err := s.byte()
		if err != nil {
			return false, err
		}
		if match && b != "name"[i] {
			match = false
		}
		if want == 0 {
			switch {
			case b < 0x80:
				continue
			case b >= 0xc2 && b <= 0xdf:
				seq[0], seqN, want = b, 1, 2
			case b >= 0xe0 && b <= 0xef:
				seq[0], seqN, want = b, 1, 3
			case b >= 0xf0 && b <= 0xf4:
				seq[0], seqN, want = b, 1, 4
			default:
				return false, &DecodeError{Code: ErrInvalidSection, Offset: s.off - 1}
			}
			continue
		}
		if b < 0x80 || b > 0xbf {
			return false, &DecodeError{Code: ErrInvalidSection, Offset: s.off - 1}
		}
		seq[seqN] = b
		seqN++
		if seqN == want {
			if !utf8.Valid(seq[:seqN]) {
				return false, &DecodeError{Code: ErrInvalidSection, Offset: s.off - seqN}
			}
			seqN, want = 0, 0
		}
	}
	if want != 0 {
		return false, &DecodeError{Code: ErrInvalidSection, Offset: start + n - seqN}
	}
	return match, nil
}

type compileStreamReader struct {
	r   io.Reader
	off int
	buf [32 << 10]byte
}

func (s *compileStreamReader) optionalByte() (byte, bool, error) {
	var b [1]byte
	n, err := s.r.Read(b[:])
	if n > 0 {
		s.off++
		return b[0], true, nil
	}
	if err == io.EOF {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return 0, false, fmt.Errorf("wasm decode: reader returned no data and no error")
}

func (s *compileStreamReader) byte() (byte, error) {
	b, ok, err := s.optionalByte()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, &DecodeError{Code: ErrIndexOutOfBounds, Offset: s.off}
	}
	return b, nil
}

func (s *compileStreamReader) bytes(n int) ([]byte, error) {
	if n < 0 {
		return nil, &DecodeError{Code: ErrIndexOutOfBounds, Offset: s.off}
	}
	b := make([]byte, n)
	if err := s.readFull(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *compileStreamReader) readFull(dst []byte) error {
	for len(dst) != 0 {
		n, err := s.r.Read(dst)
		if n > 0 {
			s.off += n
			dst = dst[n:]
		}
		if err != nil {
			if err == io.EOF && len(dst) == 0 {
				return nil
			}
			if err == io.EOF {
				return &DecodeError{Code: ErrIndexOutOfBounds, Offset: s.off}
			}
			return err
		}
		if n == 0 {
			return fmt.Errorf("wasm decode: reader returned no data and no error")
		}
	}
	return nil
}

func (s *compileStreamReader) drain(n int) error {
	for n > 0 {
		want := n
		if want > len(s.buf) {
			want = len(s.buf)
		}
		if err := s.readFull(s.buf[:want]); err != nil {
			return err
		}
		n -= want
	}
	return nil
}

func (s *compileStreamReader) u32() (uint32, error) {
	var value uint64
	for i := 0; ; i++ {
		if i >= 5 {
			return 0, &DecodeError{Code: ErrMalformedLEB, Offset: s.off}
		}
		b, err := s.byte()
		if err != nil {
			return 0, err
		}
		value |= uint64(b&0x7f) << uint(7*i)
		if b&0x80 == 0 {
			if i == 4 && b&0x70 != 0 {
				return 0, &DecodeError{Code: ErrMalformedLEB, Offset: s.off}
			}
			return uint32(value), nil
		}
	}
}

func (s *compileStreamReader) mappedPayload(n int) ([]byte, func(), error) {
	if n == 0 {
		return []byte{}, func() {}, nil
	}
	f, err := os.CreateTemp("", "wago-wasm-section-*.bin")
	if err != nil {
		return nil, nil, err
	}
	name := f.Name()
	cleanup := func() { _ = f.Close(); _ = os.Remove(name) }
	left := n
	for left > 0 {
		want := left
		if want > len(s.buf) {
			want = len(s.buf)
		}
		if err := s.readFull(s.buf[:want]); err != nil {
			cleanup()
			return nil, nil, err
		}
		if _, err := f.Write(s.buf[:want]); err != nil {
			cleanup()
			return nil, nil, err
		}
		left -= want
	}
	mem, err := syscall.Mmap(int(f.Fd()), 0, n, syscall.PROT_READ, syscall.MAP_PRIVATE)
	cleanup()
	if err != nil {
		return nil, nil, err
	}
	return mem, func() { _ = syscall.Munmap(mem) }, nil
}
