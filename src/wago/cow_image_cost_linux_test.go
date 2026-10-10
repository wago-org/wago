//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

var cowCostSink uint64

// Separate warm eligibility traversal, descriptor duplication, map/unmap,
// and first reads of mapped pages. Each arm uses the same real PHP image but
// omits unrelated import binding and PHP execution.
func BenchmarkCOWImageWarmParts(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	c = c.executionView()
	initial, maxBytes := c.memorySizeBytes()
	b.Setenv("WAGO_EXPERIMENT_COW_IMAGE", "1")
	fd, eligible, err := c.experimentalCOWImageFD(initial, maxBytes)
	if err == nil && !eligible { // first eligible use deliberately stays ordinary
		fd, eligible, err = c.experimentalCOWImageFD(initial, maxBytes)
	}
	if err != nil || !eligible {
		b.Fatalf("warm image: eligible=%t err=%v", eligible, err)
	}
	defer syscall.Close(fd)
	b.Run("scan", func(b *testing.B) {
		var sink uint64
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			var end uint64
			for i := 0; i < c.activeDataCount(); i++ {
				d := c.activeDataAt(i)
				if d.MemoryIndex != 0 || d.Offset.HasGlobal || len(d.Offset.Expr) != 0 {
					b.Fatal("PHP left constant offset class")
				}
				next := uint64(d.Offset.Base) + uint64(len(d.Bytes))
				if next > uint64(initial) {
					b.Fatal("PHP data exceeds memory")
				}
				if next > end {
					end = next
				}
			}
			sink += end
		}
		cowCostSink = sink
	})
	b.Run("warm-fd", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			next, ok, err := c.experimentalCOWImageFD(initial, maxBytes)
			if err != nil || !ok {
				b.Fatalf("duplicate image: %t %v", ok, err)
			}
			_ = syscall.Close(next)
		}
	})
	b.Run("map", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			jm, err := coreruntime.NewJobMemoryGrowableFromImage(initial, maxBytes, fd)
			if err != nil {
				b.Fatal(err)
			}
			if err := jm.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("map-read-pages", func(b *testing.B) {
		var sink byte
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			jm, err := coreruntime.NewJobMemoryGrowableFromImage(initial, maxBytes, fd)
			if err != nil {
				b.Fatal(err)
			}
			memory, err := jm.HostBytesChecked()
			if err != nil {
				b.Fatal(err)
			}
			for off := 0; off < initial; off += 4096 {
				sink ^= memory[off]
			}
			if err := jm.Close(); err != nil {
				b.Fatal(err)
			}
		}
		cowCostSink += uint64(sink)
	})
}

// Isolate first-image creation from the warm reuse path. The full-build arm
// mirrors the production sparse memfd setup and ordered segment copies;
// setup omits the copies, while scatter times only the ordered copies into a
// fresh mapping. Fresh files keep kernel first-write costs in both build arms.
func BenchmarkCOWImageBuildParts(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("../..", "corpus/workloads/applications/php/php.wasm"))
	if err != nil {
		b.Fatal(err)
	}
	c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).Compile(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	c = c.executionView()
	_, maxBytes := c.memorySizeBytes()
	var imageEnd uint64
	for i := 0; i < c.activeDataCount(); i++ {
		d := c.activeDataAt(i)
		if end := uint64(d.Offset.Base) + uint64(len(d.Bytes)); end > imageEnd {
			imageEnd = end
		}
	}
	makeMapping := func() (int, []byte) {
		fd, err := unix.MemfdCreate("wago-cow-build-cost", unix.MFD_CLOEXEC)
		if err != nil {
			b.Fatal(err)
		}
		if err := unix.Ftruncate(fd, int64(abi.BasedataSize)+int64(maxBytes)); err != nil {
			_ = syscall.Close(fd)
			b.Fatal(err)
		}
		mapped, err := syscall.Mmap(fd, 0, abi.BasedataSize+int(imageEnd),
			syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			_ = syscall.Close(fd)
			b.Fatal(err)
		}
		return fd, mapped
	}
	closeMapping := func(fd int, mapped []byte) {
		if err := syscall.Munmap(mapped); err != nil {
			b.Fatal(err)
		}
		if err := syscall.Close(fd); err != nil {
			b.Fatal(err)
		}
	}
	copySegments := func(mapped []byte) {
		for i := 0; i < c.activeDataCount(); i++ {
			d := c.activeDataAt(i)
			copy(mapped[abi.BasedataSize+int(d.Offset.Base):], d.Bytes)
		}
	}
	b.Run("setup", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			fd, mapped := makeMapping()
			closeMapping(fd, mapped)
		}
	})
	b.Run("scatter", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			fd, mapped := makeMapping()
			b.StartTimer()
			copySegments(mapped)
			b.StopTimer()
			closeMapping(fd, mapped)
			b.StartTimer()
		}
	})
	b.Run("full-build", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			fd, mapped := makeMapping()
			copySegments(mapped)
			closeMapping(fd, mapped)
		}
	})
}
