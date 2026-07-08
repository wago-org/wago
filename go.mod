module github.com/wago-org/wago

go 1.22

// The wasi plugin lives in its own repo (github.com/wago-org/wasi). The CLI
// compiles it in (with -tags wago_wasi) via this local replace against the
// sibling checkout, mirroring how wasi replaces wago => ../wago.
require github.com/wago-org/wasi v0.0.0

replace github.com/wago-org/wasi => ../wasi
