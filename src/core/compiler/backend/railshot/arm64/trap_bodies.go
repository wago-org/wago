//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

const (
	sharedTrapBodyThunkBytes = 4
	maxSharedTrapBodyShapes  = 8
)

type sharedTrapBodyInfo struct {
	off    uint32
	endOff uint32
}

func (f *fn) sharedTrapBodyInfo() sharedTrapBodyInfo {
	if f.trapBodyEnd <= f.trapBodyOff+sharedTrapBodyThunkBytes {
		return sharedTrapBodyInfo{}
	}
	return sharedTrapBodyInfo{off: uint32(f.trapBodyOff), endOff: uint32(f.trapBodyEnd)}
}

type sharedTrapBodyGroup struct {
	target int
	length int
	hash   uint64
}

// sharedTrapBodyCluster owns a bounded catalog for one run beginning at a host
// adapter and continuing through the internal functions before the next one.
// Adapter sharing can only delete bytes before both the retained boundary body
// and every later branch, so their relative displacements remain unchanged.
type sharedTrapBodyCluster struct {
	groups [maxSharedTrapBodyShapes]sharedTrapBodyGroup
	n      uint8
}

func (c *sharedTrapBodyCluster) reset() { c.n = 0 }

// share keeps the first complete body of each exact shape in place and turns
// later function-tail copies into one backward B. The first body is already a
// cold function fragment, so this achieves module reuse without a second code
// image, a whole-module deletion map, or per-function heap storage.
func (c *sharedTrapBodyCluster) share(codeBefore, fnCode []byte, entry int, info sharedTrapBodyInfo, stats *CodegenStats) []byte {
	if info.endOff == 0 || int(info.endOff) != len(fnCode) || int(info.off)+sharedTrapBodyThunkBytes >= len(fnCode) {
		return fnCode
	}
	body := fnCode[info.off:info.endOff]
	if !sharedTrapBodyPositionIndependent(body) {
		return fnCode
	}
	hash := shared.AdapterShapeHash(body, -1, 0)
	for i := 0; i < int(c.n); i++ {
		group := &c.groups[i]
		if group.hash != hash || group.length != len(body) || group.target < 0 || group.target+group.length > len(codeBefore) {
			continue
		}
		if !bytes.Equal(body, codeBefore[group.target:group.target+group.length]) {
			continue
		}
		asm := &a64.Asm{B: fnCode}
		asm.PatchU32(int(info.off), 0x14000000)
		if !asm.PatchBranch26(int(info.off), group.target-entry) {
			return fnCode
		}
		deleted := len(body) - sharedTrapBodyThunkBytes
		if stats != nil {
			stats.CodeBytes -= deleted
			stats.NativeSize.TotalBytes -= deleted
			stats.NativeSize.InternalFunctionBytes -= deleted
			stats.GCCodeBytes.Total -= deleted
			stats.GCCodeBytes.TrapStub -= deleted
			stats.peep("module-shared-trap-body")
		}
		return fnCode[:int(info.off)+sharedTrapBodyThunkBytes]
	}
	if int(c.n) < len(c.groups) {
		c.groups[c.n] = sharedTrapBodyGroup{target: entry + int(info.off), length: len(body), hash: hash}
		c.n++
	}
	return fnCode
}

func sharedTrapBodyPositionIndependent(body []byte) bool {
	if len(body)&3 != 0 {
		return false
	}
	for off := 0; off < len(body); off += 4 {
		word := binary.LittleEndian.Uint32(body[off:])
		if isPCRelativeWord(word) || word&0x3b000000 == 0x18000000 {
			return false
		}
	}
	return true
}
