//go:build !linux || !riscv64 || tinygo || wago_target_tinygo

package wago

import "github.com/wago-org/wago/src/core/compiler/wasm"

func installRV64StorageFreeProof(*Compiled, *wasm.Module) {}
func rv64StorageFreeProof(*Compiled) bool                 { return false }
func setRV64StorageFreeProof(*Compiled, bool)             {}

func rv64StructuralCodecMetadata(*Compiled) bool { return false }

func (*Compiled) rv64ExecutionContract() *rv64ExecutionContract   { return nil }
func (*Compiled) setRV64ExecutionContract(*rv64ExecutionContract) {}

func rv64ArtifactContractSource(*Compiled) bool { return false }
func rv64ArtifactFrameOwnership(*Compiled) bool { return false }
func rv64FixedRootlessGCProduct(*Compiled) bool { return false }
func rv64ArtifactRootedContract(*Compiled) bool { return false }
