package regularfile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkReadRegularFile(b *testing.B) {
	for _, size := range []int{0, 4096, 65536, 1 << 20, 8 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "data")
			if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data, err := Read(path, int64(size))
				if err != nil || len(data) != size {
					b.Fatalf("read %d: %v", len(data), err)
				}
			}
		})
	}
}
