//go:build wago_amd64_sse2

package wago

import "github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"

// The immutable baseline build profile enables conformance testing on modern
// hosts and portable artifact generation. It only removes optional features.
func selectedAMD64CompileFeatures(_ shared.AMD64Features) shared.AMD64Features { return 0 }
