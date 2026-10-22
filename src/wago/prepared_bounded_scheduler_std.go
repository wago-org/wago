//go:build (((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64)) && !tinygo

package wago

const preparedBoundedWrapperSchedulerSupported = true
