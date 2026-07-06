module wagobench

go 1.22.0

toolchain go1.22.2

require (
	github.com/tetratelabs/wazero v1.9.0
	github.com/wago-org/wago v0.1.0
	github.com/wago-org/wasi v0.0.0
)

replace github.com/wago-org/wago => ../

// wasi plugin submodule (nested module under the wago repo).
replace github.com/wago-org/wasi => ../plugins/wasi
