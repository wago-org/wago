package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkReinstallCleanup(b *testing.B) {
	for _, depth := range []int{1, 16, 64, 128} {
		b.Run(fmt.Sprint(depth), func(b *testing.B) {
			home := b.TempDir()
			root := filepath.Join(home, ".wago")
			nested := root
			for i := 0; i < depth; i++ {
				nested = filepath.Join(nested, "n")
			}
			bin, src := filepath.Join(nested, "bin"), filepath.Join(nested, "src")
			for _, path := range []string{bin, src} {
				if err := os.MkdirAll(path, 0755); err != nil {
					b.Fatal(err)
				}
			}
			in := &installer{home: home, dataDir: root, configDir: filepath.Join(root, "config"), cacheDir: filepath.Join(root, "cache"), binDir: bin, srcDir: src}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := in.cleanReinstallData("full"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
