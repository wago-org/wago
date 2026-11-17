//go:build amd64 && linux && wago_experimental_scalar_loop_entry && wago_experimental_linear_sum_outlined_poll && wago_experimental_cooperative_linear_sum && wago_experimental_bounded_vector_polls

package amd64

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestSelectiveLoopLayoutSelectors(t *testing.T) {
	for s, want := range map[string]int{"": -1, "off": -1, "0": 0, "8": 8, "16": 16, "24": 24} {
		if parseScalarLoopEntry(s) != want {
			t.Fatal(s)
		}
	}
	for s, want := range map[string]bool{"": false, "0": false, "1": true} {
		if parseLinearSumPollOutline(s) != want {
			t.Fatal(s)
		}
	}
	for _, f := range []func(){func() { parseScalarLoopEntry("32") }, func() { parseLinearSumPollOutline("2") }} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid selector accepted")
				}
			}()
			f()
		}()
	}
}

func TestSelectiveLoopLayoutFinalImage(t *testing.T) {
	old := scalarLoopEntry
	defer func() { scalarLoopEntry = old }()
	for _, offset := range []int{0, 8, 16, 24} {
		scalarLoopEntry = offset
		cm, err := CompileModuleWith(linearSumLoopModuleAMD64(t), CompileOptions{Interruptible: true, Optimizations: map[string]bool{"linear-sum-loop": false}})
		if err != nil {
			t.Fatal(err)
		}
		code := cm.Code
		sites := boundedPollInjectionSites(code)
		if len(sites) != 2 {
			t.Fatalf("expected entry/header only: %v", sites)
		}
		// This fixture's original loop poll uses RSI; prove its complete pointer
		// load rather than subtracting an unverified generic poll length.
		header := sites[1] - 4
		if header < 0 || !bytes.Equal(code[header:sites[1]], []byte{0x48, 0x8b, 0x73, 0x98}) {
			t.Fatal("unexpected header encoding")
		}
		if header&31 != offset {
			t.Fatalf("final image header=%#x target mod32=%d", header, offset)
		}
		found := false
		for i := header + 4; i+5 <= len(code); i++ {
			if code[i] == 0xe9 && i+5+int(int32(binary.LittleEndian.Uint32(code[i+1:i+5]))) == header {
				found = true
			}
		}
		if !found {
			t.Fatal("scalar backedge no longer targets original poll")
		}
		cm.CodeImage.Close()
	}
}

func TestSelectiveSumOutlinedFinalImage(t *testing.T) {
	if linearSumPollLoads <= 4 {
		t.Skip("outline applies only to sparse group policy")
	}
	old := linearSumPollOutline
	defer func() { linearSumPollOutline = old }()
	linearSumPollOutline = true
	cm, err := CompileModuleWith(linearSumLoopModuleAMD64(t), CompileOptions{Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	defer cm.CodeImage.Close()
	code := cm.Code
	sites := boundedPollInjectionSites(code)
	if len(sites) != 5 {
		t.Fatalf("need unchanged five poll sites: %v", sites)
	}
	// The exact fixture counter is R12d. Find its mask, then require a forward
	// JE to the cold block and immediate hot CMP/JAE, not an inline poll.
	mask := []byte{0x41, 0xf7, 0xc4, 0x3c, 0, 0, 0}
	at := bytes.Index(code, mask)
	if at < 0 || bytes.Count(code, mask) != 1 {
		t.Fatal("missing unique group mask")
	}
	branch := at + len(mask)
	if !bytes.Equal(code[branch:branch+2], []byte{0x0f, 0x84}) {
		t.Fatal("mask must branch to poll on zero")
	}
	target := branch + 6 + int(int32(binary.LittleEndian.Uint32(code[branch+2:branch+6])))
	compare := branch + 6
	if !bytes.Equal(code[compare:compare+6], []byte{0x41, 0x83, 0xfc, 4, 0x0f, 0x83}) {
		t.Fatal("hot compare/backedge not adjacent")
	}
	if target <= sites[3] || target >= sites[4] {
		t.Fatalf("cold block %#x not after tail %v", target, sites)
	}
	returned := false
	for i := sites[4]; i+5 <= len(code); i++ {
		if code[i] == 0xe9 && i+5+int(int32(binary.LittleEndian.Uint32(code[i+1:i+5]))) == compare {
			returned = true
			break
		}
	}
	if !returned {
		t.Fatal("cold poll does not rejoin before flag-restoring compare")
	}
}
