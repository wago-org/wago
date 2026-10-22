//go:build linux && !tinygo && !wago_profile

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

const profileEnabled = false

type profileModuleState struct{}
type profileMode struct{}

func (profileModuleState) hasFinalizedFunctionProfiles(_ []FunctionInfo, enabled bool) bool {
	return !enabled
}

func newProfileModule(bool, bool, int) profileModuleState                    { return profileModuleState{} }
func (profileModuleState) baseProfileMode() profileMode                      { return profileMode{} }
func (profileMode) moduleProfile() profileModuleState                        { return profileModuleState{} }
func (profileModuleState) newFunctionProfile(*wasm.Func, uint32) profileMode { return profileMode{} }
func (profileMode) newFunctionProfile(*wasm.Func, uint32) profileMode        { return profileMode{} }
func (*target) profileOpcode(uint32)                                         {}
func (*target) profileCold(uint32)                                           {}
func (*target) profileUnknown()                                              {}
func (*target) finishProfileBody() error                                     { return nil }
func (profileMode) finishProfileFrame(int, int, int, int) error              { return nil }
func (profileModuleState) retainProfile(*FunctionInfo) error                 { return nil }
func (profileModuleState) prepareProfile(int) error                          { return nil }
func (profileModuleState) installProfileStats(*ModuleStats)                  {}
func (f *target) compileProfileScalar(worker *shared.ScalarState, fn *wasm.Func, summary shared.ScalarSummary, widths []bool, params int) (int, error) {
	return worker.CompileScalar(fn.BodyBytes, summary, widths, params, f)
}

func (profileModuleState) prepareProfileCode([]byte, []FunctionInfo, bool) error { return nil }
