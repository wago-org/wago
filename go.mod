module github.com/wago-org/wago

go 1.22

// The wasi plugin lives in its own repo, vendored here as a git submodule at
// plugins/wasi (a nested module). The CLI compiles it in via this local replace;
// run `git submodule update --init plugins/wasi` after cloning.
require github.com/wago-org/wasi v0.0.0

replace github.com/wago-org/wasi => ./plugins/wasi
