//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import "testing"

var branchCastBenchCases = []struct {
	name     string
	sub      byte
	heap     []byte
	selector uint64
}{
	{"cast-base", 24, []byte{0}, 2},
	{"cast-subtype", 24, []byte{0}, 3},
	{"cast-miss", 24, []byte{0}, 4},
	{"fail-subtype", 25, []byte{0}, 3},
	{"fail-miss", 25, []byte{0}, 4},
	{"i31", 24, []byte{0x6c}, 1},
	{"i31-fail", 25, []byte{0x6c}, 2},
	{"none", 24, []byte{0x71}, 0},
	{"none-fail", 25, []byte{0x71}, 3},
	{"null", 25, []byte{0}, 0},
	{"fallback-struct", 24, []byte{0x6b}, 3},
}

func BenchmarkGCBranchCast(b *testing.B) {
	requireCompleteCore3Backend(b)
	const iterations = 1024
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	for _, tc := range branchCastBenchCases {
		b.Run(tc.name, func(b *testing.B) {
			c, err := Compile(config, branchCastModule(tc.sub, tc.heap, false, 0, false, false))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke("run", tc.selector, 7, iterations); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*iterations), "ns/cast")
			b.ReportMetric(float64(len(c.code)), "code-B")
		})
	}
}

func BenchmarkGCBranchCastCompile(b *testing.B) {
	requireCompleteCore3Backend(b)
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	for _, tc := range branchCastBenchCases {
		b.Run(tc.name, func(b *testing.B) {
			data := branchCastModule(tc.sub, tc.heap, false, 0, false, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(config, data)
				if err != nil {
					b.Fatal(err)
				}
				if err := c.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkGCBranchCastMixed(b *testing.B) {
	requireCompleteCore3Backend(b)
	const iterations = 512
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	instances := make([]*Instance, 2)
	for i, heap := range [][]byte{{0x6c}, {0x6b}} {
		c, err := Compile(config, branchCastModule(24, heap, false, 0, false, false))
		if err != nil {
			b.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			b.Fatal(err)
		}
		defer in.Close()
		instances[i] = in
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, in := range instances {
			if _, err := in.Invoke("run", uint64(2+i%3), 7, iterations); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*iterations*2), "ns/cast")
}
