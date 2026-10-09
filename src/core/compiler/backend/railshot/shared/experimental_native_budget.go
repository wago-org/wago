package shared

import "errors"

const (
	ExperimentMaxNativeFunctionBytes = 8192
	ExperimentMaxNativeModuleBytes   = 262144
)

var ErrExperimentalNativeBudget = errors.New("experimental native size budget")

// Total emitted bytes conservatively bound added bytes. Function checks use
// the emitter's physical span before module layout or omitted-entry aliases.
// All emitted functions are checked, including callers that inline new code.
// A failed check discards the attempt and compiles the source module.
func ExperimentalNativeFunctionBudget(codeBytes int) bool {
	return codeBytes >= 0 && codeBytes <= ExperimentMaxNativeFunctionBytes
}
func ExperimentalNativeModuleBudget(codeBytes int) bool {
	return codeBytes >= 0 && codeBytes <= ExperimentMaxNativeModuleBytes
}
