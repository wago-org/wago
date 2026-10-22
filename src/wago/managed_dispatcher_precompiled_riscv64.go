//go:build linux && riscv64 && wago_precompiled && !tinygo && !wago_target_tinygo

package wago

import _ "embed"

// This address-free dispatcher uses the calling instance's retained table and
// invocation context. The source-build test compares it with the Wasm source.
//
//go:embed internal_artifacts/managed_void_riscv64.wago
var managedVoidDispatcherArtifact []byte

func compileManagedVoidDispatcher(*RuntimeConfig) (*Compiled, error) {
	return LoadTrustedArtifact(managedVoidDispatcherArtifact)
}
