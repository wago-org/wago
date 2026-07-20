//go:build tinygo && pico2 && pico2riscv

package main

import "github.com/wago-org/wago/src/core/runtime/embedded32"

const boardTransportTarget = embedded32.TransportTargetRISCV32
