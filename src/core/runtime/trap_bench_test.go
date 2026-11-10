//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package runtime

import "testing"

var trapMessageBenchSink string

func BenchmarkTrapMessageLookup(b *testing.B) {
	var message string
	b.ReportMetric(256, "lookups/op")
	for i := 0; i < b.N; i++ {
		for j := 0; j < 256; j++ {
			message = TrapCode(j % len(trapMessages)).String()
		}
	}
	trapMessageBenchSink = message
}
