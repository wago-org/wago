//go:build linux && !tinygo

package riscv64

import (
	"encoding/hex"
	"testing"
)

func rv64GCArrayReferenceBytes(t testing.TB) []byte {
	t.Helper()
	source, err := hex.DecodeString(rv64GCArrayReferenceHex)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
