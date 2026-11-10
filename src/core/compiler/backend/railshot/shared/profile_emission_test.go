package shared

import (
	"github.com/wago-org/wago/internal/jitprofile"
	"reflect"
	"testing"
)

func TestSourceEmissionNestedAndRollback(t *testing.T) {
	var e SourceEmission
	a := EmissionOrigin{Function: 2, PC: 5, Valid: true}
	b := EmissionOrigin{Function: 7, PC: 9, Valid: true}
	e.Switch(12, a) // prologue is unknown
	e.Switch(20, b)
	e.Switch(28, a)
	e.Switch(40, EmissionOrigin{})
	e.Rewind(24) // rollback into child, then replace the rest
	e.Switch(24, a)
	e.Switch(36, EmissionOrigin{})
	e.Switch(44, b) // unknown gap
	e.Switch(48, EmissionOrigin{})
	want := []NativeSourceRange{{Offset: 12, Size: 8, Function: 2, WasmOffset: 5}, {Offset: 20, Size: 4, Function: 7, WasmOffset: 9}, {Offset: 24, Size: 12, Function: 2, WasmOffset: 5}, {Offset: 44, Size: 4, Function: 7, WasmOffset: 9}}
	if !reflect.DeepEqual(e.Ranges(), want) {
		t.Fatalf("got %+v want %+v", e.Ranges(), want)
	}
	if _, ok := jitprofile.LookupSource(e.Ranges(), 40); ok {
		t.Fatal("filled unknown gap")
	}
	e.Rewind(0)
	if len(e.Ranges()) != 0 {
		t.Fatal("rollback retained discarded code")
	}
}

func TestOverlayNativeSourcesByteOracle(t *testing.T) {
	base := []NativeSourceRange{{Offset: 2, Size: 6, Function: 1, WasmOffset: 3}, {Offset: 10, Size: 4, Function: 1, WasmOffset: 5}, {Offset: 16, Size: 8, Function: 2, WasmOffset: 7}}
	original := append([]NativeSourceRange(nil), base...)
	for start := 0; start < 28; start++ {
		for end := start + 1; end <= 28; end++ {
			checks := []NativeSourceRange{{Offset: uint64(start), Size: uint64(end - start), Function: 8, WasmOffset: 9}, {Offset: 30, Size: 2, Function: 9, WasmOffset: 10}}
			got := OverlayNativeSources(base, checks)
			if err := jitprofile.ValidateSources(got, 32); err != nil {
				t.Fatal(err)
			}
			for pc := uint64(0); pc < 32; pc++ {
				want, ok := jitprofile.LookupSource(checks, pc)
				if !ok {
					want, ok = jitprofile.LookupSource(base, pc)
				}
				actual, found := jitprofile.LookupSource(got, pc)
				if found != ok || found && (actual.Function != want.Function || actual.WasmOffset != want.WasmOffset) {
					t.Fatalf("[%d,%d) at %d got %+v %v want %+v %v", start, end, pc, actual, found, want, ok)
				}
			}
		}
	}
	if !reflect.DeepEqual(base, original) {
		t.Fatal("modified borrowed source directory")
	}
}

func TestSourceEmissionRollbackInsideActiveScope(t *testing.T) {
	var e SourceEmission
	a := EmissionOrigin{Function: 2, PC: 5, Valid: true}
	e.Switch(8, a)
	e.Rewind(16) // tentative bytes after 16 disappear; [8,16) survives
	e.Switch(20, EmissionOrigin{})
	if got := e.Ranges(); len(got) != 1 || got[0].Offset != 8 || got[0].Size != 12 {
		t.Fatal(got)
	}
}
