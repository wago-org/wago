//go:build linux && riscv64 && wago_rv64_experimental

package wago

import "github.com/wago-org/wago/codegen"

func customProducerCodegen() codegen.Lowering { return nil }
func customConsumerCodegen() codegen.Lowering { return nil }
func customCodegenAvailable() bool            { return false }
