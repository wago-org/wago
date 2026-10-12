package shared

import "github.com/wago-org/wago/src/core/compiler/wasm"

// SIMDLoadExtract describes a scalar consumer immediately after v128.load.
// The scalar read may narrow, but the original 16-byte access must still be
// bounds checked, even when the selected lane alone fits in memory.
type SIMDLoadExtract struct {
	End           int
	Offset        int32
	Size          int
	Signed, Float bool
}

func InspectSIMDLoadExtract(source *wasm.Reader) (SIMDLoadExtract, bool) {
	r := *source
	var p SIMDLoadExtract
	op, err := r.Byte()
	if err != nil || op != 0xfd {
		return p, false
	}
	sub, err := r.U32()
	if err != nil {
		return p, false
	}
	switch sub {
	case 21, 22:
		p.Size = 1
		p.Signed = sub == 21
	case 24, 25:
		p.Size = 2
		p.Signed = sub == 24
	case 27:
		p.Size = 4
	case 29:
		p.Size = 8
	case 31:
		p.Size = 4
		p.Float = true
	case 33:
		p.Size = 8
		p.Float = true
	default:
		return p, false
	}
	lane, err := r.Byte()
	if err != nil || int(lane) >= 16/p.Size {
		return p, false
	}
	p.Offset = int32(lane) * int32(p.Size)
	p.End = r.Offset()
	return p, true
}
