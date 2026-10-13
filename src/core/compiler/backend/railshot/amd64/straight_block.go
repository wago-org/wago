//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var straightLineBlockEnabled = true

func (f *fn) straightLineBlock(r *wasm.Reader, kind ctrlKind, params, results int, result machineType) bool {
	return f.opt(optStraightLineBlock) && kind == cfBlock && !f.unreachable && params == 0 && results <= 1 &&
		(results == 0 || result == mtI32 || result == mtI64 || result == mtF32 || result == mtF64 || result == mtV128) &&
		!f.moduleEH && f.gcFrameRoots == nil && len(f.customInstructions) == 0 && shared.IsStraightLineBlock(r, f.classifier)
}

// Ordinary blocks have no header branch site (-1). This distinct sentinel
// records the proven fallthrough-only frame without consuming packed flag bits.
const straightLineBlockSite = -2

func (fr *ctrlFrame) isStraightLineBlock() bool {
	return fr.kind == cfBlock && fr.controlSite == straightLineBlockSite
}
