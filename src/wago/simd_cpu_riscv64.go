//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func architectureSupportsSIMD() bool                             { return false }
func architectureSupportsBMI2() bool                             { return false }
func architectureAMD64BitCountFeatures() uint8                   { return 0 }
func architectureAMD64CPUFeatures() (shared.AMD64Features, bool) { return 0, true }
