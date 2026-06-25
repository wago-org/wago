module wagobench

go 1.22.0

toolchain go1.22.2

require (
	github.com/tetratelabs/wazero v1.9.0
	github.com/wago-org/wago v0.0.0
)

replace github.com/wago-org/wago => ../
