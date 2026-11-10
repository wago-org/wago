//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"path/filepath"
	"testing"
)

func stagedGCRefTestAbstractBytes(t testing.TB) []byte {
	t.Helper()
	var script stagedSpecScript
	tmp := stagedOfficialTypedReferenceJSON(t, "gc/ref_test", &script)
	var latest []byte
	for _, cmd := range script.Commands {
		switch cmd.Type {
		case "module_definition":
			data, err := os.ReadFile(filepath.Join(tmp, cmd.Filename))
			if err != nil {
				t.Fatal(err)
			}
			latest = data
		case "module_instance":
			pin, ok := stagedGCRefTestLeaderPinFor(latest, cmd.Line)
			if ok && pin.Class == stagedGCRefTestAbstract {
				return append([]byte(nil), latest...)
			}
		}
	}
	t.Fatal("official abstract gc/ref_test leader not found")
	return nil
}

func BenchmarkStagedGCRefTestAbstractActions(b *testing.B) {
	data := stagedGCRefTestAbstractBytes(b)
	c, err := compileStagedGCRefTestAccounting(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	extern, err := in.NewExternRef("benchmark")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := in.Invoke("init", ValueExternRef(extern).Bits()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("ref_test_any", 6)
		if err != nil || len(got) != 1 || got[0] != 2 {
			b.Fatalf("ref_test_any(6) = %v, %v", got, err)
		}
	}
}
