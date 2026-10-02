//go:build amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
)

const (
	sharedTrapBodyThunkBytesAMD64 = 5
	maxSharedTrapBodyShapesAMD64  = 8
)

type sharedTrapBodyInfoAMD64 struct {
	off    uint32
	endOff uint32
}

func (f *fn) sharedTrapBodyInfoAMD64() sharedTrapBodyInfoAMD64 {
	if f.trapBodyEnd <= f.trapBodyOff+sharedTrapBodyThunkBytesAMD64 {
		return sharedTrapBodyInfoAMD64{}
	}
	return sharedTrapBodyInfoAMD64{off: uint32(f.trapBodyOff), endOff: uint32(f.trapBodyEnd)}
}

type sharedTrapBodyGroupAMD64 struct {
	target int
	length int
	hash   uint64
}

// sharedTrapBodyClusterAMD64 owns one run beginning at a host adapter and
// continuing through the internal functions before the next one. Adapter
// sharing shifts the retained boundary body and every later branch equally.
type sharedTrapBodyClusterAMD64 struct {
	groups [maxSharedTrapBodyShapesAMD64]sharedTrapBodyGroupAMD64
	n      uint8
}

func (c *sharedTrapBodyClusterAMD64) reset() { c.n = 0 }

func (c *sharedTrapBodyClusterAMD64) shareFunction(hostAdapter bool, codeBefore, fnCode []byte, entry int, info sharedTrapBodyInfoAMD64, stats *CodegenStats) []byte {
	if hostAdapter {
		c.reset()
	}
	return c.share(codeBefore, fnCode, entry, info, stats)
}

func (c *sharedTrapBodyClusterAMD64) share(codeBefore, fnCode []byte, entry int, info sharedTrapBodyInfoAMD64, stats *CodegenStats) []byte {
	// A trailing SIMD literal pool makes the trap body non-tail. Retain that
	// function unchanged rather than moving data or rewriting RIP-relative uses.
	if info.endOff == 0 || int(info.endOff) != len(fnCode) || int(info.off)+sharedTrapBodyThunkBytesAMD64 >= len(fnCode) {
		return fnCode
	}
	body := fnCode[info.off:info.endOff]
	hash := shared.AdapterShapeHash(body, -1, 0)
	for i := 0; i < int(c.n); i++ {
		group := &c.groups[i]
		if group.hash != hash || group.length != len(body) || group.target < 0 || group.target+group.length > len(codeBefore) {
			continue
		}
		if !bytes.Equal(body, codeBefore[group.target:group.target+group.length]) {
			continue
		}
		next := int64(entry) + int64(info.off) + sharedTrapBodyThunkBytesAMD64
		delta := int64(group.target) - next
		if delta < math.MinInt32 || delta > math.MaxInt32 {
			return fnCode
		}
		fnCode[info.off] = 0xe9
		binary.LittleEndian.PutUint32(fnCode[info.off+1:], uint32(int32(delta)))
		deleted := len(body) - sharedTrapBodyThunkBytesAMD64
		if stats != nil {
			stats.CodeBytes -= deleted
			stats.NativeSize.TotalBytes -= deleted
			stats.NativeSize.InternalFunctionBytes -= deleted
			stats.GCCodeBytes.Total -= deleted
			stats.GCCodeBytes.TrapStub -= deleted
			stats.peep("module-shared-trap-body")
		}
		return fnCode[:int(info.off)+sharedTrapBodyThunkBytesAMD64]
	}
	if int(c.n) < len(c.groups) {
		c.groups[c.n] = sharedTrapBodyGroupAMD64{target: entry + int(info.off), length: len(body), hash: hash}
		c.n++
	}
	return fnCode
}
