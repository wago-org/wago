//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const rv64GCRefCastConcreteHex = "0061736d0100000001bf808080000950005f005001005f017f005001005f017f005001015f027f007f005001025f027f007f005001005f027f007f005001005f005001065f027f007f0060000003848080800003080808048480808000016b00140799808080000208746573742d73756200010a746573742d63616e6f6e00020afa8280800003ba80808000004100fb01002600410afb010026004101fb01012600410bfb010226004102fb01032600410cfb010426004103fb010526004104fb010726000bbc81808000001000d06bfb17001a41002500fb17001a41012500fb17001a41022500fb17001a41032500fb17001a41042500fb17001ad06bfb17001a41012500fb17011a41022500fb17011ad06bfb17001a41022500fb17031ad06bfb17001a41032500fb17051a41042500fb17071a41002500fb16001a41012500fb16001a41022500fb16001a41032500fb16001a41042500fb16001a41012500fb16011a41022500fb16011a41022500fb16031a41032500fb16051a41042500fb16071a0bf48080800000100041002500fb16001a41012500fb16001a41022500fb16001a41032500fb16001a41042500fb16001a410a2500fb16001a410b2500fb16001a410c2500fb16001a41012500fb16021a41022500fb16021a410b2500fb16011a410c2500fb16011a41022500fb16041a410c2500fb16031a0b"

var rv64GCRefCastConcreteSourceDigest = [sha256.Size]byte{0x65, 0xf1, 0xf3, 0x3b, 0x33, 0x5c, 0xa6, 0x2d, 0x90, 0xad, 0x08, 0x9a, 0x74, 0xf8, 0xa2, 0x9e, 0xa3, 0x16, 0x3f, 0x9a, 0x3a, 0x2f, 0x53, 0x09, 0x6b, 0xde, 0xac, 0x9e, 0x4b, 0x86, 0xf4, 0xa6}

func admitRV64GCRefCastConcreteWire(data []byte) bool {
	if len(data) != 512 || sha256.Sum256(data) != rv64GCRefCastConcreteSourceDigest || !admitExactProductWireShape(data, []byte{1, 3, 4, 7, 10}) {
		return false
	}
	m, err := wasm.DecodeModule(data)
	return err == nil && admitRV64GCRefCastConcrete(m)
}

// AdmitGCRefCastConcreteSource is the exact-wire witness for the official
// concrete struct-subtyping ref.cast checkpoint.
func AdmitGCRefCastConcreteSource(data []byte) bool { return admitRV64GCRefCastConcreteWire(data) }

func admitRV64GCRefCastConcrete(m *wasm.Module) bool {
	if !admitRV64GCRefTestConcreteShape(m) {
		return false
	}
	wantBodies := []struct {
		length int
		digest [sha256.Size]byte
	}{
		{57, [sha256.Size]byte{0xcf, 0x5f, 0xee, 0x10, 0x76, 0xc5, 0xf8, 0xf6, 0x83, 0x75, 0x06, 0x7c, 0x7b, 0x04, 0xf7, 0xf0, 0x75, 0x28, 0x8f, 0xa2, 0x7d, 0x25, 0x9a, 0x79, 0x28, 0x6a, 0x60, 0x75, 0x21, 0xde, 0x8a, 0x4f}},
		{187, [sha256.Size]byte{0x65, 0x5a, 0xd7, 0xbd, 0xa9, 0xae, 0xee, 0x7a, 0xc5, 0x8a, 0x94, 0xe7, 0xdd, 0x10, 0x15, 0x34, 0xec, 0x0a, 0xdd, 0x70, 0x36, 0x94, 0x29, 0xdf, 0xf9, 0x30, 0x13, 0x15, 0x6a, 0x96, 0x1b, 0xea}},
		{115, [sha256.Size]byte{0x1b, 0x1e, 0x2a, 0x32, 0x35, 0x14, 0x58, 0x18, 0x84, 0x6d, 0x93, 0x87, 0xb5, 0xf0, 0x06, 0x67, 0x39, 0x26, 0xbb, 0xd1, 0x7e, 0xc3, 0xf7, 0x20, 0xdc, 0x6d, 0x2b, 0x70, 0x08, 0xf4, 0x04, 0x7d}},
	}
	for i := range m.Code {
		if len(m.Code[i].BodyBytes) != wantBodies[i].length || sha256.Sum256(m.Code[i].BodyBytes) != wantBodies[i].digest {
			return false
		}
	}
	return true
}
