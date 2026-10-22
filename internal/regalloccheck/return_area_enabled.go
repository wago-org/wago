//go:build wago_regalloccheck

package regalloccheck

// ReturnArea is the single eight-byte serialized output cell of an admitted
// target ABI. It is a checked-only sink, never frame storage or an entry fact.
// Target adapters must authenticate the actual store and pointer preservation;
// this vocabulary alone does not establish any machine-code or ABI proof.
const ReturnArea Bank = Unknown + 1

func returnAreaEffectValid(e Effect) bool {
	if e.Dst != (Location{Bank: ReturnArea}) || e.Size != 8 || e.ClearTo != 0 {
		return false
	}
	switch e.Kind {
	case Copy:
		// GP index zero is a real register on some targets. A target with a
		// hardwired zero register must emit Kill for its zero-source store.
		return e.Src.Bank == GP && e.Src.Byte == 0 && flowLocation(e.Src, 8, false)
	case Kill:
		return e.Src == (Location{})
	default:
		return false
	}
}

func returnAreaUseValid(op Operation, width int) bool {
	return op.Kind == Use && op.Location == (Location{Bank: ReturnArea}) && (width == 4 || width == 8)
}
