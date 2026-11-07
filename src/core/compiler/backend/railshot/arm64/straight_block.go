//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var straightLineBlockEnabled = true

var nestedFallthroughIfEnabled = os.Getenv("WAGO_ARM64_NO_NESTED_FALLTHROUGH_IF") != "1"

func (f *fn) straightLineBlock(r *wasm.Reader, kind ctrlKind, params, results int, result machineType, signature wasm.Reader) bool {
	if !f.opt(optStraightLineBlock) || kind != cfBlock || f.unreachable || params > 8 || results > 1 ||
		(results != 0 && result != mtI32 && result != mtI64 && result != mtF32 && result != mtF64 && result != mtV128) ||
		f.moduleEH || f.gcFrameRoots != nil || len(f.customInstructions) != 0 {
		return false
	}
	// Parameterized blocks use a type index. Admit integer-only signatures;
	// machine types alone cannot distinguish i64 from a reference carrier.
	if params != 0 {
		index, err := signature.I64()
		if err != nil || index < 0 || uint64(index) > uint64(^uint32(0)) {
			return false
		}
		ft, ok := f.m.TypeFunc(uint32(index))
		if !ok {
			return false
		}
		for _, typ := range ft.Params {
			if typ != wasm.I32 && typ != wasm.I64 {
				return false
			}
		}
		for _, typ := range ft.Results {
			if typ != wasm.I32 && typ != wasm.I64 {
				return false
			}
		}
	}
	if f.makesCalls || f.inlineTargets.empty() {
		return shared.IsFallthroughBlock(r, f.classifier, nil, nestedFallthroughIfEnabled)
	}
	return shared.IsFallthroughBlock(r, f.classifier, func(index uint32) bool {
		t := f.inlineTargets.target(int(index))
		_, reserved := f.inlineBase[int(index)]
		return reserved && t != nil && (t.integerPairLeaf() || f.inlineTupleFallsThrough(t))
	}, nestedFallthroughIfEnabled)
}

// Ordinary blocks have no header branch site (-1). This distinct sentinel
// records the proven fallthrough-only frame without consuming packed flag bits.
const straightLineBlockSite = -2

func (fr *ctrlFrame) isStraightLineBlock() bool {
	return fr.kind == cfBlock && fr.controlSite == straightLineBlockSite
}
