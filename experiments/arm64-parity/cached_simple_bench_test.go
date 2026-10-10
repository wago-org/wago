package parity

import (
	"encoding/json"
	wago "github.com/wago-org/wago"
	"os"
	"path/filepath"
	"testing"
)

// Focused cached-core timing; callers explicitly select a contract. Complex
// memory/vector/host contracts use their dedicated oracle harness instead.
func BenchmarkCachedSimple(b *testing.B) {
	root, id := os.Getenv("WAGO_PARITY_CACHE"), os.Getenv("WAGO_PARITY_CORE_ID")
	if root == "" || id == "" {
		b.Skip("select a cached simple core contract")
	}
	var m struct {
		Workloads []struct {
			ID, Artifact, ABI, Export string
			HostProfile               string `json:"host_profile"`
			Initialize                string
			Input                     json.RawMessage
			Args                      []json.RawMessage
			Oracle                    struct {
				Kind     string
				Expected []json.RawMessage
				Memory   []json.RawMessage
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		b.Fatal(err)
	}
	if err = json.Unmarshal(data, &m); err != nil {
		b.Fatal(err)
	}
	for _, w := range m.Workloads {
		if w.ID != id {
			continue
		}
		if w.ABI != "core" || w.HostProfile != "" || w.Initialize != "" || len(w.Input) != 0 || w.Oracle.Kind != "exact_u64" || len(w.Oracle.Memory) != 0 {
			b.Fatal("requires a dedicated timing harness")
		}
		code, err := os.ReadFile(filepath.Join(root, w.Artifact))
		if err != nil {
			b.Fatal(err)
		}
		args := make([]uint64, len(w.Args))
		for i, v := range w.Args {
			args[i], err = parseCachedUint(v)
			if err != nil {
				b.Fatal(err)
			}
		}
		want := make([]uint64, len(w.Oracle.Expected))
		for i, v := range w.Oracle.Expected {
			want[i], err = parseCachedUint(v)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.Run("Compile", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c, err := wago.Compile(nil, code)
				if err != nil {
					b.Fatal(err)
				}
				if err = c.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("Exec", func(b *testing.B) {
			c, err := wago.Compile(nil, code)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := wago.Instantiate(c, wago.InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			f, err := in.WasmFunc(w.Export)
			if err != nil {
				b.Fatal(err)
			}
			check := func() {
				out, err := f.Invoke(args...)
				if err != nil {
					b.Fatal(err)
				}
				if len(out) != len(want) {
					b.Fatal("oracle result count")
				}
				for i, v := range out {
					if v != want[i] {
						b.Fatalf("result %d got %d want %d", i, v, want[i])
					}
				}
			}
			for i := 0; i < 5; i++ {
				check()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err = f.Invoke(args...); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			check()
		})
		return
	}
	b.Fatalf("cached contract %q not found", id)
}
