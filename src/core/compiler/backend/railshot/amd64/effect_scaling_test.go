//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"testing"
)

func BenchmarkCompileInertPrefixStores(b *testing.B) {
	const n = 8192
	body := []byte{0}
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 1)
	}
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 0, 0x41, 1, 0x36, 2, 0)
	}
	for i := 0; i < n; i++ {
		body = append(body, 0x1a)
	}
	body = append(body, 0x0b)
	m := benchDecodeValidateModule(b, benchModuleBytes([]benchFuncDef{{body: body}}, true))
	benchmarkCompileModule(b, m)
}
