//go:build arm64

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func architectureSupportsBMI2() bool { return false }

func architectureAMD64CPUFeatures() (shared.AMD64Features, bool) { return 0, true }
