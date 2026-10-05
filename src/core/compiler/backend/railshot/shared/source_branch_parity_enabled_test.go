//go:build wago_regalloccheck

package shared

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

// Typed control images are decoded only. Incorrect guest bytes never execute.
func TestSourceBranchIndexedEncoderControls(t *testing.T) {
	for c := byte(0); c < 2; c++ {
		for then := byte(0); then < 2; then++ {
			for otherwise := byte(0); otherwise < 2; otherwise++ {
				for _, arm := range []bool{false, true} {
					for _, mode := range []string{"positive", "wrong condition", "wrong then", "wrong else", "wrong return", "positive zero", "wrong condition zero", "wrong then zero", "wrong else zero", "wrong return zero"} {
						zero := strings.HasSuffix(mode, " zero")
						if zero && !arm {
							continue
						}
						t.Run(fmt.Sprintf("arm%v/%d%d%d/%s", arm, c, then, otherwise, mode), func(t *testing.T) {
							mode := strings.TrimSuffix(mode, " zero")
							s, m := sourceBranchIndexedFixture(t, c, then, otherwise)
							b := BeginSourceBranch(s, m, 0, true)
							if b == nil {
								t.Fatal("indexed family not admitted")
							}
							defer b.Close()
							condition, thenSrc, elseSrc := c, then, otherwise
							switch mode {
							case "wrong condition":
								condition ^= 1
							case "wrong then":
								thenSrc ^= 1
							case "wrong else":
								elseSrc ^= 1
							}
							var code []byte
							if arm {
								var a a64.Asm
								a.ObserveRegalloc(b.ObserveEffect)
								a.ObserveGPWrites(b.ObserveGPWrites)
								emitSourceBranchARMFrame(&a, false)
								if !zero {
									a.Store32(a64.X0, a64.SP, 0)
									a.Store32(a64.X1, a64.SP, 8)
								}
								branch := a.Cbz32(a64.Reg(condition))
								if zero {
									a.MovReg64(a64.X12, a64.Reg(thenSrc))
								} else {
									a.Load32(a64.X12, a64.SP, uint32(thenSrc)*8)
								}
								jump := a.Branch()
								falseStart := a.Len()
								if zero {
									a.MovReg64(a64.X12, a64.Reg(elseSrc))
								} else {
									a.Load32(a64.X12, a64.SP, uint32(elseSrc)*8)
								}
								join := a.Len()
								src := a64.X12
								if mode == "wrong return" {
									src = a64.Reg(then ^ 1)
								}
								a.MovReg32(a64.X0, src)
								restore := a.Len()
								emitSourceBranchARMFrame(&a, true)
								if zero {
									for _, at := range []int{0, restore} {
										for j := 0; j < 12; j += 4 {
											a.PatchU32(at+j, 0xd503201f)
										}
									}
								}
								a.Ret()
								a.PatchBranch19(branch, falseStart)
								a.PatchBranch26(jump, join)
								b.EndEmission(a.Len())
								code = a.B
							} else {
								var a x86.Asm
								a.ObserveRegalloc(b.ObserveEffect)
								a.ObserveGPWrites(b.ObserveGPWrites)
								a.SubRsp(24)
								a.MovRegReg32(x86.R9, x86.RAX)
								a.MovRegReg32(x86.R10, x86.RCX)
								pins := [2]x86.Reg{x86.R9, x86.R10}
								a.Store32(x86.RSP, 8, pins[condition])
								a.Load32(x86.RDI, x86.RSP, 8)
								a.TestSelf(x86.RDI, false)
								branch := a.JccPlaceholder(x86.CondE)
								a.MovRegReg32(x86.RBP, pins[thenSrc])
								jump := a.JmpPlaceholder()
								falseStart := a.Len()
								a.MovRegReg32(x86.RBP, pins[elseSrc])
								join := a.Len()
								src := x86.RBP
								if mode == "wrong return" {
									src = pins[then^1]
								}
								a.MovRegReg32(x86.RAX, src)
								a.AddRsp(24)
								a.Ret()
								a.PatchRel32(branch, falseStart)
								a.PatchRel32(jump, join)
								b.EndEmission(a.Len())
								code = a.B
							}
							want := regalloccheck.Rejected
							if mode == "positive" {
								want = regalloccheck.Verified
							}
							if r := b.Verify(code, arm); r.Verdict != want {
								t.Fatalf("result=%+v code=%x", r, code)
							}
						})
					}
				}
			}
		}
	}
}

func emitSourceBranchARMFrame(a *a64.Asm, restore bool) {
	a.Movz64(a64.X16, 16, 0)
	a.Movk64(a64.X16, 0, 1)
	if restore {
		a.AddSPReg(a64.X16)
	} else {
		a.SubSPReg(a64.X16)
	}
}

func TestSourceBranchARMEncodingAndObserverControls(t *testing.T) {
	for _, mode := range []string{"original", "short frame", "cmp zero", "wide stores", "normalized args", "frame bounds", "SP balance", "unaligned frame", "wrong predicate", "wide condition", "wrong false target", "wrong join target", "missing observer", "raw gap", "mixed rewrite", "changed length", "reserved destination", "wrong return control"} {
		t.Run(mode, func(t *testing.T) {
			s, m := sourceBranchFixture(t)
			b := BeginSourceBranch(s, m, 0, true)
			if b == nil {
				t.Fatal("not admitted")
			}
			defer b.Close()
			var a a64.Asm
			a.ObserveRegalloc(b.ObserveEffect)
			a.ObserveGPWrites(b.ObserveGPWrites)
			if mode == "missing observer" {
				a.ObserveGPWrites(nil)
			}
			emitSourceBranchARMFrame(&a, false)
			if mode == "normalized args" {
				a.MovReg32(a64.X0, a64.X0)
				a.MovReg32(a64.X1, a64.X1)
			}
			if mode == "wide stores" {
				a.Store64(a64.X0, a64.SP, 0)
				a.Store64(a64.X1, a64.SP, 8)
			} else {
				a.Store32(a64.X0, a64.SP, 0)
				a.Store32(a64.X1, a64.SP, 8)
			}
			if mode == "raw gap" {
				a.B = append(a.B, 0x1f, 0x20, 0x03, 0xd5)
			}
			var branch int
			switch mode {
			case "cmp zero":
				a.CmpImm32(a64.X0, 0)
				branch = a.Bcond(a64.CondEQ)
			case "wrong predicate":
				branch = a.Cbnz32(a64.X0)
			case "wide condition":
				branch = a.Cbz64(a64.X0)
			default:
				branch = a.Cbz32(a64.X0)
			}
			off := uint32(8)
			if mode == "frame bounds" {
				off = 16
			}
			merge := a64.X12
			if mode == "reserved destination" {
				merge = a64.X18
			}
			a.Load32(merge, a64.SP, off)
			jump := a.Branch()
			falseStart := a.Len()
			a.Load32(merge, a64.SP, 0)
			join := a.Len()
			a.MovReg32(a64.X0, merge)
			restore := a.Len()
			emitSourceBranchARMFrame(&a, true)
			a.Ret()
			if mode == "SP balance" {
				a.PatchMovImm(restore, 32)
			}
			if mode == "unaligned frame" {
				a.PatchMovImm(0, 20)
				a.PatchMovImm(restore, 20)
			}
			if mode == "short frame" || mode == "mixed rewrite" {
				for _, pc := range []int{0, restore} {
					w := uint32(0xd10043ff)
					if pc == restore {
						w = 0x910043ff
					}
					a.PatchU32(pc, w)
					a.PatchU32(pc+4, 0xd503201f)
					if mode != "mixed rewrite" {
						a.PatchU32(pc+8, 0xd503201f)
					}
				}
			}
			if mode == "wrong false target" {
				falseStart += 4
			}
			if mode == "wrong join target" {
				join += 4
			}
			if mode == "wrong return control" {
				a.PatchU32(a.Len()-4, 0xd65f0000)
			}
			a.PatchBranch19(branch, falseStart)
			a.PatchBranch26(jump, join)
			b.EndEmission(a.Len())
			code := a.B
			if mode == "changed length" {
				code = code[:len(code)-4]
			}
			want := regalloccheck.Inconclusive
			switch mode {
			case "original", "short frame", "cmp zero", "wide stores", "normalized args":
				want = regalloccheck.Verified
			case "frame bounds", "SP balance", "unaligned frame":
				want = regalloccheck.Rejected
			}
			if r := b.Verify(code, true); r.Verdict != want {
				t.Fatalf("result=%+v code=%x", r, code)
			}
		})
	}
}
