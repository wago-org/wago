//go:build !linux || !riscv64 || wago_standalone || tinygo || wago_target_tinygo

package wago

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func railshotConfigureFrontendTable64(*frontend.Features) {}

func validateRV64GenericTable64Source(*wasm.Module) error {
	return fmt.Errorf("compile: generic RV64 table64 backend is unavailable")
}
