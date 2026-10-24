//go:build arm64

package arm64

import "testing"

func TestTrapSourcePackingPreservesWideCoordinatesArm64(t *testing.T) {
	sc := newCompileScratch(minStackArenaCap)
	f := fn{sc: sc}
	functions := []uint32{0, 1, compactTrapFuncLimit - 1, compactTrapFuncLimit, 1<<16 - 1, ^uint32(0)}
	pcs := []uint32{0, 1, compactTrapPCMask, compactTrapPCMask + 1, ^uint32(0)}
	var sites []trapSite
	wide := 0
	for _, function := range functions {
		for _, pc := range pcs {
			f.traceFuncIdx, f.wasmPC = function, pc
			site := f.trapSite(16)
			if f.trapFunction(site) != function || f.trapPC(site) != pc || site.branch != 16 {
				t.Fatalf("source %d/%d changed to %d/%d", function, pc, f.trapFunction(site), f.trapPC(site))
			}
			if function >= compactTrapFuncLimit || pc > compactTrapPCMask {
				wide++
			}
			// Reverse order makes sorting exercise compact and escaped sources.
			sites = append([]trapSite{site}, sites...)
		}
	}
	if len(sc.wideTrapSources) != wide {
		t.Fatal("compact coordinates allocated escape storage")
	}
	f.sortTrapSitesByFunction(sites)
	for i := 1; i < len(sites); i++ {
		if f.trapFunction(sites[i]) < f.trapFunction(sites[i-1]) {
			t.Fatal("trap groups no longer sort by complete function index")
		}
	}
	sc.reset()
	if len(sc.wideTrapSources) != 0 {
		t.Fatal("function reset retained previous escape entries")
	}
}
