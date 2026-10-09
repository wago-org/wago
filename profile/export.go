// Package profile exports Wago code-image metadata to existing profiling tools.
// It contains no sampler and does not claim guest stack-unwinding support.
package profile

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"

	"github.com/wago-org/wago/internal/jitprofile"
)

// Symbol includes source, compilation, mapping generation and full function
// identity. Text fields are display text only. ASCII escaping prevents format injection.
func Symbol(im jitprofile.Image, r jitprofile.Region) string {
	clean := func(s string) string {
		return strings.Map(func(c rune) rune {
			if c > 126 || c < 33 || unicode.IsSpace(c) {
				return '_'
			}
			return c
		}, s)
	}
	return fmt.Sprintf("wago:%s:%s:g%d:f%d:r%x:%s:%s", clean(im.ModuleID), clean(im.ArtifactID), im.ID, r.Function, r.Offset, clean(r.Kind), clean(r.Name))
}

func executable(r jitprofile.Region) bool { return r.Kind != "padding" && r.Kind != "literal-data" }

func writeAll(w io.Writer, b []byte) error {
	n, err := w.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return err
}

// PerfMap appends all mappings to one writer. Address reuse fails explicitly:
// perf maps cannot represent temporal generations. Use jitdump for such captures.
type PerfMap struct {
	w     io.Writer
	spans [][2]uint64
	err   error
}

func NewPerfMap(w io.Writer) *PerfMap { return &PerfMap{w: w} }
func (p *PerfMap) Write(events []jitprofile.Event) error {
	if p.err != nil {
		return p.err
	}
	for _, e := range events {
		if e.Kind != "load" || e.Image == nil {
			continue
		}
		im := e.Image
		if im.Base > math.MaxUint64-im.Size {
			p.err = fmt.Errorf("profile image address overflow")
			return p.err
		}
		for _, old := range p.spans {
			if im.Base < old[1] && old[0] < im.Base+im.Size {
				p.err = fmt.Errorf("perf-map cannot represent reused address %#x; use jitdump", im.Base)
				return p.err
			}
		}
		if err := jitprofile.ValidateRegions(im.Regions, im.Size); err != nil {
			p.err = err
			return err
		}
		p.spans = append(p.spans, [2]uint64{im.Base, im.Base + im.Size})
		for _, r := range im.Regions {
			if executable(r) {
				p.err = writeAll(p.w, []byte(fmt.Sprintf("%x %x %s\n", im.Base+r.Offset, r.Size, Symbol(*im, r))))
				if p.err != nil {
					return p.err
				}
			}
		}
	}
	return nil
}

// JITDump writes Linux perf jitdump records. The caller must also keep the dump
// file executable-mapped in the profiled process (OpenJITDump does this on Linux).
// This encoder is portable so format validation does not require perf or Linux.
type JITDump struct {
	unwindPending                                      bool
	unwindImage, unwindAddress, unwindSize, unwindTime uint64
	w                                                  io.Writer
	pid                                                uint32
	arch                                               string
	index                                              uint64
	err                                                error
	closed                                             bool
}

func NewJITDump(w io.Writer, pid uint32, arch string, timestamp uint64) (*JITDump, error) {
	var machine uint32
	switch arch {
	case "amd64":
		machine = 62
	case "arm64":
		machine = 183
	default:
		return nil, fmt.Errorf("unsupported jitdump architecture %q", arch)
	}
	h := make([]byte, 40)
	put32(h, 0, 0x4a695444)
	put32(h, 4, 1)
	put32(h, 8, 40)
	put32(h, 12, machine)
	put32(h, 20, pid)
	put64(h, 24, timestamp)
	if err := writeAll(w, h); err != nil {
		return nil, err
	}
	return &JITDump{w: w, pid: pid, arch: arch}, nil
}
func put32(b []byte, at int, n uint32) { binary.LittleEndian.PutUint32(b[at:], n) }
func put64(b []byte, at int, n uint64) { binary.LittleEndian.PutUint64(b[at:], n) }
func (j *JITDump) Write(events []jitprofile.Event) error {
	if j.err != nil {
		return j.err
	}
	if j.closed {
		return fmt.Errorf("jitdump is closed")
	}
	for _, e := range events {
		if e.Kind != "load" || e.Image == nil {
			continue
		} // No per-function unload record exists.
		im := e.Image
		if im.Base > math.MaxUint64-im.Size {
			j.err = fmt.Errorf("jitdump image address overflow")
			return j.err
		}
		if err := jitprofile.ValidateRegions(im.Regions, im.Size); err != nil {
			j.err = err
			return err
		}
		if uint64(len(im.Code)) != im.Size {
			j.err = fmt.Errorf("jitdump requires native bytes; enable IncludeCode")
			return j.err
		}
		if err := jitprofile.ValidateUnwind(im.Unwind, im.Size); err != nil {
			j.err = err
			return err
		}
		if len(im.Unwind) != 0 && (j.arch != "amd64" || !strings.HasSuffix(im.Target, "/amd64")) {
			j.err = fmt.Errorf("unsupported compiler unwind target %q in %s jitdump", im.Target, j.arch)
			return j.err
		}
		unwindAt := 0
		for _, r := range im.Regions {
			first := unwindAt
			for unwindAt < len(im.Unwind) && im.Unwind[unwindAt].Offset < r.Offset+r.Size {
				row := im.Unwind[unwindAt]
				if !executable(r) || row.Offset < r.Offset || row.Size > r.Offset+r.Size-row.Offset {
					j.err = fmt.Errorf("unwind rule crosses a code region or covers non-executable bytes")
					return j.err
				}
				unwindAt++
			}
			if unwindAt > first {
				rows := append([]jitprofile.UnwindRange(nil), im.Unwind[first:unwindAt]...)
				for i := range rows {
					rows[i].Offset -= r.Offset
				}
				info, err := EncodeAMD64Unwind(r.Size, rows)
				if err != nil {
					j.err = err
					return err
				}
				if err = j.WriteUnwind(im.ID, im.Base+r.Offset, r.Size, info, e.Timestamp); err != nil {
					return err
				}
			}
			if executable(r) {
				if j.unwindPending && (j.unwindImage != im.ID || j.unwindImage != e.ImageID || j.unwindAddress != im.Base+r.Offset || j.unwindSize != r.Size || j.unwindTime > e.Timestamp) {
					j.err = fmt.Errorf("jitdump unwind target does not match next code load")
					return j.err
				}
				name := Symbol(*im, r)
				size := uint64(56+len(name)+1) + r.Size
				if size > math.MaxUint32 || im.Base > math.MaxUint64-r.Offset {
					j.err = fmt.Errorf("jitdump record overflow")
					return j.err
				}
				b := make([]byte, 56)
				put32(b, 0, 0)
				put32(b, 4, uint32(size))
				put64(b, 8, e.Timestamp)
				put32(b, 16, j.pid)
				tid := e.ThreadID
				if tid == 0 {
					tid = j.pid
				}
				put32(b, 20, tid)
				put64(b, 24, im.Base+r.Offset)
				put64(b, 32, im.Base+r.Offset)
				put64(b, 40, r.Size)
				j.unwindPending = false
				j.index++
				put64(b, 48, j.index)
				for _, part := range [][]byte{b, append([]byte(name), 0), im.Code[r.Offset : r.Offset+r.Size]} {
					if err := writeAll(j.w, part); err != nil {
						j.err = err
						return err
					}
				}
			}
		}
	}
	return nil
}
func (j *JITDump) Close(timestamp uint64) error {
	if j.err != nil {
		return j.err
	}
	if j.closed {
		return nil
	}
	if j.unwindPending {
		j.err = fmt.Errorf("jitdump closes with unwind metadata awaiting code")
		return j.err
	}
	j.closed = true
	b := make([]byte, 16)
	put32(b, 0, 3)
	put32(b, 4, 16)
	put64(b, 8, timestamp)
	j.err = writeAll(j.w, b)
	return j.err
}
