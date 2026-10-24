//go:build !linux || !riscv64 || wago_precompiled || tinygo || wago_target_tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/frontend"
)

func railshotConfigureFrontendTable64(*frontend.Features) {}
