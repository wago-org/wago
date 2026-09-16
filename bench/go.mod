module github.com/wago-org/wago/bench

go 1.22.0

toolchain go1.22.2

require (
	github.com/tetratelabs/wazero v1.9.0
	github.com/wago-org/wago v0.1.0
	github.com/wago-org/wasi v0.3.1-0.20260916034125-6a6684d2ecd2
)

require golang.org/x/sys v0.30.0 // indirect

replace github.com/wago-org/wago => ../
