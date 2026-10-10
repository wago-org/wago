//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestCOWImagePrivateMapping is an isolated OS oracle, not Wago integration.
// It verifies that source image bytes are shared for reads and copied on write.
func TestCOWImagePrivateMapping(t *testing.T) {
	for _, rel := range []string{
		"corpus/workloads/applications/sqlite3/sqlite3.wasm",
		"corpus/workloads/applications/php/php.wasm",
	} {
		t.Run(filepath.Base(rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", rel))
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var end uint64
			for i := 0; i < c.activeDataCount(); i++ {
				d := c.activeDataAt(i)
				if d.MemoryIndex != 0 || d.Offset.HasGlobal || len(d.Offset.Expr) != 0 {
					t.Fatal("fixture left narrow constant-offset class")
				}
				if n := uint64(d.Offset.Base) + uint64(len(d.Bytes)); n > end {
					end = n
				}
			}
			f, err := os.CreateTemp(t.TempDir(), "wago-cow-image-*")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := f.Truncate(int64(end)); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < c.activeDataCount(); i++ {
				d := c.activeDataAt(i)
				if len(d.Bytes) == 0 {
					continue
				}
				if n, err := f.WriteAt(d.Bytes, int64(d.Offset.Base)); err != nil || n != len(d.Bytes) {
					t.Fatalf("image segment %d write = %d, %v", i, n, err)
				}
			}
			a, err := syscall.Mmap(int(f.Fd()), 0, int(end), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Munmap(a)
			b, err := syscall.Mmap(int(f.Fd()), 0, int(end), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Munmap(b)
			for i := 0; i < c.activeDataCount(); i++ {
				d := c.activeDataAt(i)
				if len(d.Bytes) == 0 {
					continue
				}
				off := int(d.Offset.Base)
				if a[off] != d.Bytes[0] || b[off] != d.Bytes[0] {
					t.Fatalf("segment %d did not share initial image", i)
				}
			}
			probe := int(c.activeDataAt(0).Offset.Base)
			original := b[probe]
			a[probe] ^= 0xff
			if b[probe] != original {
				t.Fatal("private mapping leaked write to sibling")
			}
			var source [1]byte
			if _, err := f.ReadAt(source[:], int64(probe)); err != nil || source[0] != original {
				t.Fatal("private mapping changed backing image")
			}
			t.Logf("image_bytes=%d segments=%d private_instances=2 read_and_write_isolation=pass", end, c.activeDataCount())
		})
	}
}

func imageMappingPSSKB(path string) (int, error) {
	data, err := os.ReadFile("/proc/self/smaps")
	if err != nil {
		return 0, err
	}
	pss, matched := 0, false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && strings.Contains(fields[0], "-") && len(fields[1]) == 4 && strings.HasSuffix(fields[1], "p") {
			matched = strings.Contains(line, path)
		} else if matched && len(fields) >= 2 && fields[0] == "Pss:" {
			n, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, err
			}
			pss += n
		}
	}
	return pss, nil
}

// This process-level mapping-specific PSS probe is directional. It does not
// include the compiled module's retained source bytes or Wago's other mappings.
func TestCOWImageMultiInstancePSS(t *testing.T) {
	for _, tc := range []struct {
		rel    string
		counts []int
	}{
		{"corpus/workloads/applications/sqlite3/sqlite3.wasm", []int{1, 10, 100}},
		{"corpus/workloads/applications/php/php.wasm", []int{1, 10}},
	} {
		t.Run(filepath.Base(tc.rel), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../..", tc.rel))
			if err != nil {
				t.Fatal(err)
			}
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var end uint64
			pages := map[uint64]bool{}
			for i := 0; i < c.activeDataCount(); i++ {
				d := c.activeDataAt(i)
				n := uint64(d.Offset.Base) + uint64(len(d.Bytes))
				if n > end {
					end = n
				}
				if len(d.Bytes) > 0 {
					for p := uint64(d.Offset.Base) / 4096; p <= (n-1)/4096; p++ {
						pages[p] = true
					}
				}
			}
			indices := make([]uint64, 0, len(pages))
			for p := range pages {
				indices = append(indices, p)
			}
			sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
			f, err := os.CreateTemp(t.TempDir(), "wago-cow-pss-*")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := f.Truncate(int64(end)); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < c.activeDataCount(); i++ {
				d := c.activeDataAt(i)
				if len(d.Bytes) > 0 {
					if _, err := f.WriteAt(d.Bytes, int64(d.Offset.Base)); err != nil {
						t.Fatal(err)
					}
				}
			}
			var mappings [][]byte
			defer func() {
				for _, m := range mappings {
					_ = syscall.Munmap(m)
				}
			}()
			var sink byte
			for _, count := range tc.counts {
				for len(mappings) < count {
					m, err := syscall.Mmap(int(f.Fd()), 0, int(end), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
					if err != nil {
						t.Fatal(err)
					}
					mappings = append(mappings, m)
					for _, p := range indices {
						sink ^= m[p*4096]
					}
				}
				pss, err := imageMappingPSSKB(f.Name())
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("instances=%d virtual_bytes=%d touched_pages_per_instance=%d mapping_pss_kb=%d sink=%d", count, count*int(end), len(indices), pss, sink)
			}
		})
	}
}

var cowImageBenchSink byte

// BenchmarkCOWImagePrototype compares only the image operation, excluding
// compilation, file construction, table/global setup, and Wago instantiation.
func BenchmarkCOWImagePrototype(b *testing.B) {
	for _, tc := range []struct{ name, rel string }{
		{"sqlite", "corpus/workloads/applications/sqlite3/sqlite3.wasm"},
		{"php", "corpus/workloads/applications/php/php.wasm"},
	} {
		data, err := os.ReadFile(filepath.Join("../..", tc.rel))
		if err != nil {
			b.Fatal(err)
		}
		c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(data)
		if err != nil {
			b.Fatal(err)
		}
		var end uint64
		pages := map[uint64]bool{}
		for i := 0; i < c.activeDataCount(); i++ {
			d := c.activeDataAt(i)
			if n := uint64(d.Offset.Base) + uint64(len(d.Bytes)); n > end {
				end = n
			}
			if len(d.Bytes) > 0 {
				for p := uint64(d.Offset.Base) / 4096; p <= (uint64(d.Offset.Base)+uint64(len(d.Bytes))-1)/4096; p++ {
					pages[p] = true
				}
			}
		}
		pageIndices := make([]uint64, 0, len(pages))
		for p := range pages {
			pageIndices = append(pageIndices, p)
		}
		sort.Slice(pageIndices, func(i, j int) bool { return pageIndices[i] < pageIndices[j] })
		f, err := os.CreateTemp(b.TempDir(), "wago-cow-bench-*")
		if err != nil {
			b.Fatal(err)
		}
		if err := f.Truncate(int64(end)); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < c.activeDataCount(); i++ {
			d := c.activeDataAt(i)
			if len(d.Bytes) > 0 {
				if _, err := f.WriteAt(d.Bytes, int64(d.Offset.Base)); err != nil {
					b.Fatal(err)
				}
			}
		}
		b.Run(tc.name+"/private_map", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := syscall.Mmap(int(f.Fd()), 0, int(end), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
				if err != nil {
					b.Fatal(err)
				}
				cowImageBenchSink ^= m[0]
				if err := syscall.Munmap(m); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(tc.name+"/private_map_read_pages", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := syscall.Mmap(int(f.Fd()), 0, int(end), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
				if err != nil {
					b.Fatal(err)
				}
				for _, p := range pageIndices {
					cowImageBenchSink ^= m[p*4096]
				}
				if err := syscall.Munmap(m); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(tc.name+"/private_map_first_write", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m, err := syscall.Mmap(int(f.Fd()), 0, int(end), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE)
				if err != nil {
					b.Fatal(err)
				}
				m[pageIndices[0]*4096] ^= 1
				if err := syscall.Munmap(m); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(tc.name+"/copy_segments", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				buf := make([]byte, end)
				for j := 0; j < c.activeDataCount(); j++ {
					d := c.activeDataAt(j)
					copy(buf[d.Offset.Base:], d.Bytes)
				}
				cowImageBenchSink ^= buf[0]
			}
		})
		_ = f.Close()
		_ = c.Close()
	}
}
