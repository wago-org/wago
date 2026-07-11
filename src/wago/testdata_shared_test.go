//go:build (linux && amd64) || arm64

package wago

import (
	"os"
	"path/filepath"
)

// testdata reads a checked-in test module. Broadly tagged (rather than living in
// the historically linux&&amd64 wago_test.go) so arch-neutral tests that consume
// these fixtures can be widened to arm64.
func testdata(name string) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "testdata", name))
	if err != nil {
		panic(err)
	}
	return b
}

var memprogWasm = testdata("memprog.wasm")
