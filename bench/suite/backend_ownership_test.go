package wagobench

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

type benchCloseProbe struct{ calls int }

func (p *benchCloseProbe) Take() ([]byte, uintptr, error) { return nil, 0, fmt.Errorf("not supported") }
func (p *benchCloseProbe) Close() error                   { p.calls++; return nil }

func TestBenchCompiledOwner(t *testing.T) {
	p := &benchCloseProbe{}
	m := &benchCompiledModule{Code: []byte{1}, Entry: []int{0}, image: p}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || m.Code != nil || m.Entry != nil || m.image != nil {
		t.Fatalf("owner released %d times or views retained", p.calls)
	}
}

func TestBenchBackendRelease(t *testing.T) {
	m, err := wasm.DecodeModule(fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			for i := 0; i < 128; i++ {
				cm, err := benchCompileModuleWorkers(m, workers)
				if err != nil {
					t.Fatal(err)
				}
				if len(cm.Code) == 0 {
					_ = cm.Close()
					t.Fatal("empty code")
				}
				image := cm.image
				if workers == 1 && image == nil {
					t.Fatal("serial image owner discarded")
				}
				if err := cm.Close(); err != nil {
					t.Fatal(err)
				}
				if image != nil {
					if _, _, err := image.Take(); err == nil {
						t.Fatal("released image remained available")
					}
				}
			}
		})
	}
}
