//go:build !riscv64

package wago

func rv64GCStructMutationExecutionCode(*Compiled) bool { return false }
func rv64GCStructMutation(*Compiled) bool              { return false }
