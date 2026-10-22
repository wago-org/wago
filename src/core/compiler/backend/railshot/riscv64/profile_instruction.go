//go:build linux && !tinygo && wago_profile

package riscv64

import (
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

// Abstract states are bounded per instruction and reused per function. A merge
// may only lose information. SP is relative to function-entry SP; RA is the
// address of its proved stack cell relative to the same caller CFA.
type profileStackState struct {
	sp, ra, c0 int32
	flags      uint8
}

const (
	profileSeen uint8 = 1 << iota
	profileSPKnown
	profileRAKnown
	profileConstantKnown
	profileCallerRA
)

func mergeProfileStack(a, b profileStackState) profileStackState {
	if a.flags&profileSeen == 0 {
		return b
	}
	out := a
	if a.flags&profileSPKnown == 0 || b.flags&profileSPKnown == 0 || a.sp != b.sp {
		out.flags &^= profileSPKnown | profileRAKnown
		out.sp = 0
		out.ra = 0
	}
	if a.flags&profileRAKnown == 0 || b.flags&profileRAKnown == 0 || a.ra != b.ra {
		out.flags &^= profileRAKnown
		out.ra = 0
	}
	if a.flags&profileConstantKnown == 0 || b.flags&profileConstantKnown == 0 || a.c0 != b.c0 {
		out.flags &^= profileConstantKnown
		out.c0 = 0
	}
	if a.flags&profileCallerRA == 0 || b.flags&profileCallerRA == 0 {
		out.flags &^= profileCallerRA
	}
	return out
}
func profileSigned(v uint32, bits uint) int32 { return int32(v<<(32-bits)) >> (32 - bits) }
func profileBranchOffset(w uint32) int32 {
	return profileSigned((w>>31)<<12|(w>>7&1)<<11|(w>>25&63)<<5|(w>>8&15)<<1, 13)
}
func profileJumpOffset(w uint32) int32 {
	return profileSigned((w>>31)<<20|(w>>12&255)<<12|(w>>20&1)<<11|(w>>21&1023)<<1, 21)
}
func profileStoreOffset(w uint32) int32 { return profileSigned(w>>25<<5|(w>>7&31), 12) }

func profileInstructionMetadata(code []byte, functions []FunctionInfo, regions []jitprofile.Region, wantSites, wantUnwind bool) ([]shared.NativeCodeSite, []jitprofile.UnwindRange, error) {
	if !wantSites && !wantUnwind {
		return nil, nil, nil
	}
	if len(code) > maxProfileCodeBytes {
		return nil, nil, fmt.Errorf("rv64: instruction profile code bound")
	}
	var sites []shared.NativeCodeSite
	var unwind []jitprofile.UnwindRange
	var scratch []profileStackState
	var queue []uint32
	// Frame-site output dominates metadata storage. Count physical candidates
	// once to reserve exact capacity rather than repeatedly copying a growing
	// slice. Unreachable candidates remain spare capacity, not published sites.
	if wantSites {
		count := 0
		for _, fn := range functions {
			if fn.Entry < 0 || fn.Entry&3 != 0 || fn.CodeBytes < 0 || fn.CodeBytes&3 != 0 || fn.Entry > len(code)-fn.CodeBytes {
				return nil, nil, fmt.Errorf("rv64: invalid instruction profile function span")
			}
			for at := fn.Entry; at < fn.Entry+fn.CodeBytes; at += 4 {
				w := binary.LittleEndian.Uint32(code[at:])
				if w>>15&31 == 2 && (w&127 == 0x03 || w&127 == 0x23 || w&127 == 0x07 || w&127 == 0x27) {
					count++
				}
			}
			if count > maxProfileModuleRanges {
				return nil, nil, fmt.Errorf("rv64: instruction profile metadata bound")
			}
		}
		if count != 0 {
			sites = make([]shared.NativeCodeSite, 0, count)
		}
	}
	for _, fn := range functions {
		if fn.Entry < 0 || fn.Entry&3 != 0 || fn.CodeBytes < 0 || fn.CodeBytes&3 != 0 || fn.Entry > len(code)-fn.CodeBytes {
			return nil, nil, fmt.Errorf("rv64: invalid instruction profile function span")
		}
		body := code[fn.Entry : fn.Entry+fn.CodeBytes]
		n := len(body) / 4
		if n == 0 {
			continue
		}
		if cap(scratch) < n {
			scratch = make([]profileStackState, n)
			queue = make([]uint32, 0, n)
		} else {
			scratch = scratch[:n]
			clear(scratch)
		}
		queue = queue[:0]
		scratch[0] = profileStackState{flags: profileSeen | profileSPKnown | profileCallerRA}
		queue = append(queue, 0)
		enqueue := func(index int, state profileStackState) {
			if index < 0 || index >= n {
				return
			}
			next := mergeProfileStack(scratch[index], state)
			if next != scratch[index] {
				scratch[index] = next
				queue = append(queue, uint32(index))
			}
		}
		for at := 0; at < len(queue); at++ {
			index := int(queue[at])
			state := scratch[index]
			w := binary.LittleEndian.Uint32(body[index*4:])
			op := w & 127
			rd, rs1, rs2 := w>>7&31, w>>15&31, w>>20&31
			funct := w >> 12 & 7
			next := state
			// Only canonical RV64 frame constants authorize arithmetic SP changes.
			if rd == 30 && op != 0x23 && op != 0x63 {
				next.flags &^= profileConstantKnown
				next.c0 = 0
				switch {
				case op == 0x37:
					next.c0 = int32(w & 0xfffff000)
					next.flags |= profileConstantKnown
				case (op == 0x13 || op == 0x1b) && funct == 0 && rs1 == 0:
					next.c0 = int32(w) >> 20
					next.flags |= profileConstantKnown
				case (op == 0x13 || op == 0x1b) && funct == 0 && rs1 == 30 && state.flags&profileConstantKnown != 0:
					next.c0 = state.c0 + (int32(w) >> 20)
					next.flags |= profileConstantKnown
				}
			}
			if rd == 2 && op != 0x23 && op != 0x63 {
				known := false
				delta := int32(0)
				if op == 0x13 && funct == 0 && rs1 == 2 {
					known = true
					delta = int32(w) >> 20
				}
				if op == 0x33 && funct == 0 && rs1 == 2 && rs2 == 30 && state.flags&profileConstantKnown != 0 {
					known = true
					delta = state.c0
					if w>>25 == 32 {
						delta = -delta
					} else if w>>25 != 0 {
						known = false
					}
				}
				value := int64(state.sp) + int64(delta)
				if !known || state.flags&profileSPKnown == 0 || value > 0 || value < -MaxFrameBytes*2 {
					next.flags &^= profileSPKnown | profileRAKnown
					next.sp = 0
					next.ra = 0
				} else {
					next.sp = int32(value)
				}
			}
			if (op == 0x23 || op == 0x27) && rs1 == 2 && state.flags&profileSPKnown != 0 {
				off := state.sp + profileStoreOffset(w)
				if op == 0x23 && funct == 3 && rs2 == 1 && off < 0 && state.flags&profileCallerRA != 0 {
					next.ra = off
					next.flags |= profileRAKnown
				} else if state.flags&profileRAKnown != 0 {
					width := int32(1 << funct)
					if funct <= 3 && off < state.ra+8 && state.ra < off+width {
						next.flags &^= profileRAKnown
						next.ra = 0
					}
				}
			}
			// The instruction pass has no address-range proof for a store
			// through another register. Trusted full-access plugins may alias
			// the frame, so retain no saved-cell claim across that write.
			if ((op == 0x23 || op == 0x27) && rs1 != 2) || op == 0x2f {
				next.flags &^= profileRAKnown
				next.ra = 0
			}
			if rd == 1 && op != 0x23 && op != 0x63 && op != 0x07 && op != 0x27 && op != 0x53 {
				next.flags &^= profileCallerRA
				if op == 0x03 && funct == 3 && rs1 == 2 && state.flags&(profileSPKnown|profileRAKnown) == profileSPKnown|profileRAKnown && state.sp+(int32(w)>>20) == state.ra {
					next.flags |= profileCallerRA
				}
			}
			if (op == 0x6f || op == 0x67) && rd != 0 {
				next.flags &^= profileConstantKnown
				next.c0 = 0
			}
			switch op {
			case 0x63:
				enqueue(index+int(profileBranchOffset(w))/4, next)
				enqueue(index+1, next)
			case 0x6f:
				if rd == 0 {
					enqueue(index+int(profileJumpOffset(w))/4, next)
				} else {
					enqueue(index+1, next)
				}
			case 0x67:
				if rd != 0 {
					enqueue(index+1, next)
				} else if index > 0 {
					prev := binary.LittleEndian.Uint32(body[(index-1)*4:])
					if prev&127 == 0x17 && prev>>7&31 == rs1 {
						dest := int64((index-1)*4) + int64(int32(prev&0xfffff000)) + int64(int32(w)>>20)
						if dest%4 == 0 {
							enqueue(int(dest/4), next)
						}
					}
				}
			default:
				enqueue(index+1, next)
			}
		}
		for i, state := range scratch {
			offset := uint64(fn.Entry + i*4)
			w := binary.LittleEndian.Uint32(body[i*4:])
			op := w & 127
			rd, rs1, rs2 := w>>7&31, w>>15&31, w>>20&31
			funct := w >> 12 & 7
			if wantUnwind && state.flags&(profileSeen|profileSPKnown|profileRAKnown) == profileSeen|profileSPKnown|profileRAKnown && state.sp < 0 && state.ra < 0 && state.ra >= state.sp {
				r := jitprofile.UnwindRange{Offset: offset, Size: 4, CFARegister: 2, CFAOffset: -int64(state.sp), ReturnOffset: int64(state.ra)}
				if len(unwind) > 0 {
					last := &unwind[len(unwind)-1]
					if last.Offset+last.Size == r.Offset && last.CFARegister == r.CFARegister && last.CFAOffset == r.CFAOffset && last.ReturnOffset == r.ReturnOffset {
						last.Size += 4
					}
				}
				// Coalescing must not skip an independently authored site at this word.
				if len(unwind) == 0 || unwind[len(unwind)-1].Offset+unwind[len(unwind)-1].Size != offset+4 {
					unwind = append(unwind, r)
				}
			}
			if wantSites && state.flags&profileSeen != 0 && rs1 == 2 {
				kind := ""
				if op == 0x03 || op == 0x07 {
					kind = "frame-load"
					if op == 0x03 && rd == 1 && funct == 3 {
						kind = "return-address-restore"
					}
				} else if op == 0x23 || op == 0x27 {
					kind = "frame-store"
					if op == 0x23 && rs2 == 1 && funct == 3 {
						kind = "return-address-save"
					}
				}
				if kind != "" {
					sites = append(sites, shared.NativeCodeSite{Offset: offset, Size: 4, Kind: kind})
				}
			}
			if len(sites) > maxProfileModuleRanges || len(unwind) > maxProfileModuleRanges {
				return nil, nil, fmt.Errorf("rv64: instruction profile metadata bound")
			}
		}
	}
	if err := jitprofile.ValidateUnwind(unwind, uint64(len(code))); err != nil {
		return nil, nil, err
	}
	if err := jitprofile.ValidateCodeSites(sites, uint64(len(code))); err != nil {
		return nil, nil, err
	}
	if err := jitprofile.ValidateCodeSiteRegions(sites, regions); err != nil {
		return nil, nil, err
	}
	return sites, unwind, nil
}
