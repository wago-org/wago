package arm32

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// CompileF32BitFunction lowers raw-bit f32 constants, locals, abs, neg,
// copysign, nop, and drop through the qualified one-word scalar backend.
func CompileF32BitFunction(numParams int, body []byte) ([]byte, error) {
	translated, err := shared.TranslateF32BitBody(numParams, body)
	if err != nil {
		return nil, err
	}
	return CompileBeachhead(numParams, translated)
}
