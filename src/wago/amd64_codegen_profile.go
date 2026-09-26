//go:build !wago_amd64_sse2

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

func selectedAMD64CompileFeatures(host shared.AMD64Features) shared.AMD64Features { return host }
