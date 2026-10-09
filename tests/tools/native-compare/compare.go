// Package main implements an offline diagnostic experiment. No production code
// imports it. Input instruction boundaries and source anchors come from the
// existing disassembler/source maps, not from an invented general decoder.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

const maxRegions = 64
const maxInstructions = 4096
const maxChanges = 512
const maxInputBytes = 1 << 20

type Provenance struct {
	CompilerRevision string `json:"compiler_revision"`
	BinarySHA256     string `json:"binary_sha256"`
	InputSHA256      string `json:"input_sha256"`
	CPUFeatures      string `json:"cpu_features"`
	Bounds           string `json:"bounds"`
	Build            string `json:"build"`
	Path             string `json:"compiler_path"`
}
type Instruction struct {
	Offset uint64 `json:"offset"`
	Hex    string `json:"hex"`
	// Relocation names a stable logical target, never an unstable address/name.
	// It is admitted only for direct JMP/Jcc (AMD64) or B (ARM64).
	Relocation string `json:"relocation,omitempty"`
}
type Region struct {
	ID           string        `json:"id"`
	Function     uint32        `json:"function"`
	WasmOffset   *uint32       `json:"wasm_offset"`
	Instructions []Instruction `json:"instructions"`
}
type CaptureMetadata struct {
	NativeSHA256  string `json:"native_sha256"`
	NativeBytes   uint64 `json:"native_bytes"`
	MappedBytes   uint64 `json:"mapped_bytes"`
	UnmappedBytes uint64 `json:"unmapped_bytes"`
	RawCoverage   bool   `json:"raw_coverage,omitempty"`
}

type Snapshot struct {
	Capture      *CaptureMetadata `json:"capture,omitempty"`
	Architecture string           `json:"architecture"`
	Provenance   Provenance       `json:"provenance"`
	Regions      []Region         `json:"regions"`
	RawRegions   []RawRegion      `json:"raw_regions,omitempty"`
}
type Register struct {
	Number int `json:"number"`
	Width  int `json:"width"`
}

// Address retains a base register and signed displacement without formatting
// strings per instruction. SP distinguishes ARM64 register 31 from XZR.
type Address struct {
	Displacement int64 `json:"displacement"`
	Base         uint8 `json:"base_register"`
	Present      bool  `json:"present"`
	SP           bool  `json:"stack_pointer"`
}

type Record struct {
	ReadCount      int         `json:"read_count"`
	WriteCount     int         `json:"write_count"`
	Encoding       string      `json:"comparison_encoding"`
	Raw            string      `json:"raw"`
	Opcode         string      `json:"opcode"`
	Reads          [2]Register `json:"reads,omitempty"`
	Writes         [1]Register `json:"writes,omitempty"`
	Width          int         `json:"width,omitempty"`
	Immediate      uint64      `json:"immediate_bits,omitempty"`
	Address        Address     `json:"address,omitempty"`
	ImmediateWidth int         `json:"immediate_width,omitempty"`
	Target         string      `json:"target,omitempty"`
	Known          bool        `json:"known"`
	ZeroExtends    bool        `json:"zero_extends,omitempty"`
	FlagsRead      bool        `json:"flags_read,omitempty"`
	FlagsWrite     bool        `json:"flags_write,omitempty"`
}
type Change struct {
	Category     string  `json:"category"`
	Region       string  `json:"region"`
	Function     uint32  `json:"function"`
	WasmOffset   *uint32 `json:"wasm_offset"`
	BeforeOffset uint64  `json:"before_offset"`
	AfterOffset  uint64  `json:"after_offset"`
	Before       Record  `json:"before"`
	After        Record  `json:"after"`
}
type UnknownSite struct {
	Region       string  `json:"region"`
	Function     uint32  `json:"function"`
	WasmOffset   *uint32 `json:"wasm_offset"`
	BeforeOffset uint64  `json:"before_offset"`
	AfterOffset  uint64  `json:"after_offset"`
	BeforeHex    string  `json:"before_hex"`
	AfterHex     string  `json:"after_hex"`
}

type Report struct {
	OperandScope  string           `json:"operand_scope"`
	RawComplete   bool             `json:"raw_complete"`
	RawCompared   int              `json:"raw_compared"`
	RawChanges    []RawChange      `json:"raw_changes,omitempty"`
	BeforeCapture *CaptureMetadata `json:"before_capture,omitempty"`
	AfterCapture  *CaptureMetadata `json:"after_capture,omitempty"`
	UnknownSites  []UnknownSite    `json:"unknown_sites,omitempty"`
	Complete      bool             `json:"complete"`
	Compared      int              `json:"compared"`
	Known         int              `json:"known"`
	Unknown       int              `json:"unknown"`
	Changes       []Change         `json:"changes"`
	Limit         bool             `json:"limit"`
	Reasons       []string         `json:"reasons,omitempty"`
}

func validate(s Snapshot) error {
	if s.Architecture != "amd64" && s.Architecture != "arm64" {
		return fmt.Errorf("unsupported architecture")
	}
	if len(s.Regions) == 0 || len(s.Regions) > maxRegions {
		return fmt.Errorf("region budget/empty snapshot")
	}
	count := 0
	var mappedBytes uint64
	var previousEnd uint64
	ids := make(map[string]bool, len(s.Regions))
	for ri, r := range s.Regions {
		if r.ID == "" || len(r.ID) > 128 || ids[r.ID] {
			return fmt.Errorf("missing/duplicate/long region identity")
		}
		ids[r.ID] = true
		count += len(r.Instructions)
		if count > maxInstructions {
			return fmt.Errorf("instruction budget")
		}
		if len(r.Instructions) == 0 {
			return fmt.Errorf("empty region")
		}
		if ri > 0 && r.Instructions[0].Offset < previousEnd {
			return fmt.Errorf("overlapping/out-of-order native regions")
		}
		var end uint64
		for i, in := range r.Instructions {
			if len(in.Relocation) > 128 {
				return fmt.Errorf("target identity budget")
			}
			n := len(in.Hex) / 2
			if len(in.Hex)%2 != 0 || n == 0 || n > 15 || s.Architecture == "arm64" && (n != 4 || in.Offset%4 != 0) {
				return fmt.Errorf("instruction size/alignment")
			}
			if in.Offset > ^uint64(0)-uint64(n) || i > 0 && in.Offset != end {
				return fmt.Errorf("non-contiguous instruction boundaries")
			}
			end = in.Offset + uint64(n)
			mappedBytes += uint64(n)
			if s.Capture != nil && end > s.Capture.NativeBytes {
				return fmt.Errorf("instruction outside captured image")
			}
			var storage [15]byte
			if _, err := hex.Decode(storage[:n], []byte(in.Hex)); err != nil {
				return fmt.Errorf("invalid instruction hex")
			}
		}
		previousEnd = end
	}
	if c := s.Capture; c != nil {
		var hash [32]byte
		if len(c.NativeSHA256) != 64 {
			return fmt.Errorf("native hash length")
		}
		if _, err := hex.Decode(hash[:], []byte(c.NativeSHA256)); err != nil {
			return fmt.Errorf("native hash encoding")
		}
		if c.NativeBytes == 0 || c.MappedBytes > c.NativeBytes || c.UnmappedBytes != c.NativeBytes-c.MappedBytes || c.MappedBytes != mappedBytes {
			return fmt.Errorf("inconsistent capture byte counts")
		}
	}
	return validateRaw(s)
}

func provenanceComplete(p Provenance) bool {
	for _, s := range []string{p.BinarySHA256, p.InputSHA256} {
		var storage [32]byte
		if len(s) != 64 {
			return false
		}
		n, err := hex.Decode(storage[:], []byte(s))
		if err != nil || n != 32 {
			return false
		}
	}
	return p.CompilerRevision != "" && p.CompilerRevision != "unqualified-build" && p.CPUFeatures != "" && p.Bounds != "" && p.Build != "" && p.Path != ""
}

// Compare aligns matching named source regions, then instruction positions.
// Insertions/region mismatch are inconclusive rather than guessed alignment.
// Runtime is linear in admitted input size; no LCS or quadratic search is used.
func Compare(a, b Snapshot) (Report, error) {
	out := Report{OperandScope: "supplied-source-regions", Complete: true, Changes: make([]Change, 0), BeforeCapture: a.Capture, AfterCapture: b.Capture}
	if err := validate(a); err != nil {
		return out, err
	}
	if err := validate(b); err != nil {
		return out, err
	}
	incomplete := func(reason string) { out.Complete = false; out.Reasons = append(out.Reasons, reason) }
	if a.Architecture != b.Architecture {
		return out, fmt.Errorf("architecture mismatch")
	}
	if !provenanceComplete(a.Provenance) || !provenanceComplete(b.Provenance) {
		incomplete("missing or unqualified provenance")
	}
	ap, bp := a.Provenance, b.Provenance
	if ap.CPUFeatures != bp.CPUFeatures || ap.Bounds != bp.Bounds || ap.Build != bp.Build || ap.Path != bp.Path || ap.InputSHA256 != bp.InputSHA256 {
		incomplete("configuration/input mismatch")
	}
	compareRaw(a, b, &out)
	if !out.RawComplete && (a.Capture != nil && a.Capture.RawCoverage || b.Capture != nil && b.Capture.RawCoverage) {
		incomplete("raw region coverage/alignment incomplete")
	}
	if len(a.Regions) != len(b.Regions) {
		incomplete("region count mismatch")
		return out, nil
	}
	for i, ar := range a.Regions {
		br := b.Regions[i]
		if ar.ID != br.ID || ar.Function != br.Function || !samePC(ar.WasmOffset, br.WasmOffset) || len(ar.Instructions) != len(br.Instructions) {
			incomplete("ambiguous region alignment: " + ar.ID)
			continue
		}
		if ar.WasmOffset == nil {
			incomplete("unmapped source region: " + ar.ID)
		}
		for j, ai := range ar.Instructions {
			bi := br.Instructions[j]
			av, err := decode(a.Architecture, ai)
			if err != nil {
				return out, err
			}
			identical := ai.Relocation == bi.Relocation && strings.EqualFold(ai.Hex, bi.Hex)
			bv := av
			if !identical {
				bv, err = decode(b.Architecture, bi)
				if err != nil {
					return out, err
				}
			}
			out.Compared++
			if av.Known && bv.Known {
				out.Known++
			} else {
				out.Unknown++
				out.Complete = false
				if len(out.UnknownSites) == maxChanges {
					out.Limit = true
					incomplete("unknown-site budget")
					return out, nil
				}
				out.UnknownSites = append(out.UnknownSites, UnknownSite{ar.ID, ar.Function, ar.WasmOffset, ai.Offset, bi.Offset, av.Raw, bv.Raw})
			}
			if identical {
				continue
			}
			ac, bc := av, bv
			ac.Raw = ""
			bc.Raw = ""
			if ac != bc {
				if len(out.Changes) == maxChanges {
					out.Limit = true
					incomplete("change budget")
					return out, nil
				}
				out.Changes = append(out.Changes, Change{classify(av, bv), ar.ID, ar.Function, ar.WasmOffset, ai.Offset, bi.Offset, av, bv})
			}
		}
	}
	if out.Unknown > 0 {
		out.Reasons = append(out.Reasons, "unknown instruction semantics")
	}
	return out, nil
}

func decode(arch string, in Instruction) (Record, error) {
	var storage [15]byte
	b := storage[:len(in.Hex)/2]
	_, _ = hex.Decode(b, []byte(in.Hex))
	raw := strings.ToLower(in.Hex)
	r := Record{Raw: raw, Opcode: "unknown"}
	if arch == "amd64" {
		r = decodeAMD64(b, raw)
	} else {
		r = decodeARM64(b, raw)
	}
	r.Encoding = r.Raw
	if in.Relocation != "" {
		if !r.Known || !branchRecord(r) {
			return r, fmt.Errorf("relocation is not an admitted direct branch")
		}
		if arch == "arm64" {
			r.Encoding = "00000014"
		} else if r.Opcode == "jmp" {
			if len(b) == 2 {
				r.Encoding = "eb00"
			} else {
				r.Encoding = "e900000000"
			}
		} else if len(b) == 2 {
			r.Encoding = jccShort[b[0]&15]
		} else {
			r.Encoding = jccNear[b[1]&15]
		}
		r.Target = in.Relocation
		r.Immediate = 0
	}
	return r, nil
}

func decodeAMD64(b []byte, raw string) Record {
	unknown := Record{Raw: raw, Opcode: "unknown"}
	r := unknown
	// Exact canonical alignment NOP forms only. No memory read or flag effects.
	if raw == "660f1f840000000000" || raw == "0f1f00" || raw == "90" {
		r.Known = true
		r.Opcode = "nop"
		return r
	}
	if b[0] == 0xe9 && len(b) == 5 {
		r.Known = true
		r.Opcode = "jmp"
		r.Immediate = uint64(binary.LittleEndian.Uint32(b[1:]))
		r.ImmediateWidth = 32
		return r
	}
	if b[0] == 0xeb && len(b) == 2 {
		r.Known = true
		r.Opcode = "jmp"
		r.Immediate = uint64(b[1])
		r.ImmediateWidth = 8
		return r
	}
	if b[0] >= 0x70 && b[0] <= 0x7f && len(b) == 2 {
		r.Known = true
		r.Opcode = jccOpcodes[b[0]&15]
		r.FlagsRead = true
		r.Immediate = uint64(b[1])
		r.ImmediateWidth = 8
		return r
	}
	if b[0] == 0x0f && len(b) == 6 && b[1] >= 0x80 && b[1] <= 0x8f {
		r.Known = true
		r.Opcode = jccOpcodes[b[1]&15]
		r.FlagsRead = true
		r.Immediate = uint64(binary.LittleEndian.Uint32(b[2:]))
		r.ImmediateWidth = 32
		return r
	}
	at, rex, width := 0, byte(0), 32
	if b[0]&0xf0 == 0x40 {
		rex = b[0]
		at++
		if rex&8 != 0 {
			width = 64
		}
	}
	if at >= len(b) {
		return unknown
	}
	op := b[at]
	at++
	if op >= 0xb8 && op <= 0xbf && len(b)-at == width/8 && rex&6 == 0 {
		r.Known = true
		r.Opcode = "mov-immediate"
		r.Width = width
		r.ImmediateWidth = width
		r.Writes = [1]Register{{int(op-0xb8) + int(rex&1)*8, width}}
		r.WriteCount = 1
		r.ZeroExtends = width == 32
		if width == 32 {
			r.Immediate = uint64(binary.LittleEndian.Uint32(b[at:]))
		} else {
			r.Immediate = binary.LittleEndian.Uint64(b[at:])
		}
		return r
	}
	if at >= len(b) {
		return unknown
	}
	modrm := b[at]
	at++
	reg := int(modrm>>3&7) + int(rex>>2&1)*8
	rm := int(modrm&7) + int(rex&1)*8
	if (op == 0x81 || op == 0x83) && modrm>>6 == 3 && rex&6 == 0 {
		group := modrm >> 3 & 7
		if group != 0 && group != 5 && group != 7 {
			return unknown
		}
		if op == 0x83 && len(b)-at == 1 {
			r.Immediate = uint64(int64(int8(b[at])))
			r.ImmediateWidth = 8
		} else if op == 0x81 && len(b)-at == 4 {
			r.Immediate = uint64(int64(int32(binary.LittleEndian.Uint32(b[at:]))))
			r.ImmediateWidth = 32
		} else {
			return unknown
		}
		if width == 32 {
			r.Immediate &= 0xffffffff
		}
		r.Known = true
		r.Width = width
		r.Reads = [2]Register{{rm, width}}
		r.ReadCount = 1
		r.FlagsWrite = true
		r.Opcode = "add-immediate"
		if group == 5 {
			r.Opcode = "sub-immediate"
		}
		if group == 7 {
			r.Opcode = "cmp-immediate"
		} else {
			r.Writes = [1]Register{{rm, width}}
			r.WriteCount = 1
			r.ZeroExtends = width == 32
		}
		return r
	}
	if op != 0x89 && op != 0x8b && op != 0x31 && op != 0x01 && op != 0x39 {
		return unknown
	}
	r.Width = width
	if modrm>>6 == 3 && at == len(b) && rex&2 == 0 {
		dst, src := rm, reg
		if op == 0x8b {
			dst, src = reg, rm
		}
		r.Known = true
		r.Writes = [1]Register{{dst, width}}
		r.WriteCount = 1
		r.ZeroExtends = width == 32
		if op == 0x89 || op == 0x8b {
			r.Opcode = "mov-register"
			r.Reads = [2]Register{{src, width}}
			r.ReadCount = 1
		} else {
			r.FlagsWrite = true
			r.Opcode = "add-register"
			r.Reads = [2]Register{{dst, width}, {src, width}}
			r.ReadCount = 2
			if op == 0x39 {
				r.Opcode = "cmp-register"
				r.Writes = [1]Register{}
				r.WriteCount = 0
				r.ZeroExtends = false
			}
			if op == 0x31 {
				r.Opcode = "xor-register"
				if dst == src {
					r.Reads = [2]Register{}
					r.ReadCount = 0
				}
			}
		}
		return r
	}
	if (op != 0x89 && op != 0x8b) || modrm>>6 == 3 || rex&2 != 0 {
		return unknown
	}
	if modrm>>6 == 0 && modrm&7 == 5 {
		return unknown
	} // RIP-relative excluded.
	if modrm&7 == 4 {
		if at >= len(b) || b[at] != 0x24 {
			return unknown
		}
		at++
	} // No index.
	disp := int64(0)
	switch modrm >> 6 {
	case 0:
		if at != len(b) {
			return unknown
		}
	case 1:
		if len(b)-at != 1 {
			return unknown
		}
		disp = int64(int8(b[at]))
	case 2:
		if len(b)-at != 4 {
			return unknown
		}
		disp = int64(int32(binary.LittleEndian.Uint32(b[at:])))
	}
	r.Known = true
	r.Address = Address{Displacement: disp, Base: uint8(rm), Present: true, SP: rm == 4}
	if op == 0x89 {
		r.Opcode = "store"
		r.Reads = [2]Register{{rm, 64}, {reg, width}}
		r.ReadCount = 2
	} else {
		r.Opcode = "load"
		r.Reads = [2]Register{{rm, 64}}
		r.ReadCount = 1
		r.Writes = [1]Register{{reg, width}}
		r.WriteCount = 1
		r.ZeroExtends = width == 32
	}
	return r
}

var jccOpcodes = [16]string{"jcc-0", "jcc-1", "jcc-2", "jcc-3", "jcc-4", "jcc-5", "jcc-6", "jcc-7", "jcc-8", "jcc-9", "jcc-a", "jcc-b", "jcc-c", "jcc-d", "jcc-e", "jcc-f"}

func branchRecord(r Record) bool {
	return r.Opcode == "jmp" || r.Opcode == "b" || strings.HasPrefix(r.Opcode, "jcc-")
}
func classify(a, b Record) string {
	if !a.Known || !b.Known {
		return "unknown"
	}
	if branchRecord(a) || branchRecord(b) {
		return "branch"
	}
	if a.Width != b.Width || a.ZeroExtends != b.ZeroExtends {
		return "width"
	}
	if a.Address != b.Address {
		return "addressing"
	}
	if a.Immediate != b.Immediate || a.ImmediateWidth != b.ImmediateWidth {
		return "constant"
	}
	if a.Reads != b.Reads || a.ReadCount != b.ReadCount || a.Writes != b.Writes || a.WriteCount != b.WriteCount || a.FlagsRead != b.FlagsRead || a.FlagsWrite != b.FlagsWrite {
		return "register-dependency"
	}
	return "instruction"
}

func decodeARM64(b []byte, raw string) Record {
	r := Record{Raw: raw, Opcode: "unknown"}
	w := binary.LittleEndian.Uint32(b)
	width := 32
	if w>>31 != 0 {
		width = 64
	}
	if w&0xfc000000 == 0x14000000 {
		r.Known = true
		r.Opcode = "b"

		r.Immediate = uint64(w & 0x03ffffff)
		return r
	}
	rd, rn := int(w&31), int(w>>5&31)
	if w&0x7f800000 == 0x52800000 {
		shift := int(w>>21&3) * 16
		if shift >= width {
			return r
		}
		r.Known = true
		r.Opcode = "movz"
		r.Width = width
		r.Immediate = uint64(w>>5&0xffff) << shift
		if rd != 31 {
			r.Writes = [1]Register{{rd, width}}
			r.WriteCount = 1
			r.ZeroExtends = width == 32
		}
		return r
	}
	if w&0x7f800000 == 0x11000000 && rd != 31 && rn != 31 {
		r.Known = true
		r.Opcode = "add-immediate"
		r.Width = width
		r.Immediate = uint64(w >> 10 & 0xfff)
		if w&(1<<22) != 0 {
			r.Immediate <<= 12
		}
		r.Reads = [2]Register{{rn, width}}
		r.ReadCount = 1
		r.Writes = [1]Register{{rd, width}}
		r.WriteCount = 1
		r.ZeroExtends = width == 32
		return r
	}
	// Unsigned-immediate GP load/store. Reject SIMD and other addressing modes.
	if w&0xbfc00000 == 0xb9000000 || w&0xbfc00000 == 0xb9400000 {
		width = 32
		if w&(1<<30) != 0 {
			width = 64
		}
		r.Width = width
		r.Known = true
		r.Address = Address{Displacement: int64(w>>10&0xfff) * int64(width/8), Base: uint8(rn), Present: true, SP: rn == 31}
		r.Reads = [2]Register{{rn, 64}}
		r.ReadCount = 1
		r.Opcode = "load"
		if w&(1<<22) == 0 {
			r.Opcode = "store"
			if rd != 31 {
				r.Reads[1] = Register{rd, width}
				r.ReadCount = 2
			}
		} else if rd != 31 {
			r.Writes = [1]Register{{rd, width}}
			r.WriteCount = 1
			r.ZeroExtends = width == 32
		}
		return r
	}
	return r
}

func samePC(a, b *uint32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

var jccShort = [16]string{"7000", "7100", "7200", "7300", "7400", "7500", "7600", "7700", "7800", "7900", "7a00", "7b00", "7c00", "7d00", "7e00", "7f00"}
var jccNear = [16]string{"0f8000000000", "0f8100000000", "0f8200000000", "0f8300000000", "0f8400000000", "0f8500000000", "0f8600000000", "0f8700000000", "0f8800000000", "0f8900000000", "0f8a00000000", "0f8b00000000", "0f8c00000000", "0f8d00000000", "0f8e00000000", "0f8f00000000"}
