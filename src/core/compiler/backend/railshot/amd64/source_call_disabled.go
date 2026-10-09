//go:build amd64 && wago_regalloccheck && (tinygo || wago_profile)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
)

func checkSourceCallsFinal(*wasm.Module, CompileOptions, *encoder.CompiledModule) error { return nil }
