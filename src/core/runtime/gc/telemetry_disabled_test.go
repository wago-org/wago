//go:build !wago_gcstats

package gc

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDisabledTelemetryHooksRemainInert(t *testing.T) {
	telemetry := new(Telemetry)
	c, err := NewCollector(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if allocs := testing.AllocsPerRun(1000, func() {
		telemetry.attach(ProfileThroughput, 1)
		telemetry.setGlobalRootClass(2, RootPublicToken)
		if telemetry.globalRootClass(2) != RootGlobal {
			panic("disabled root classification changed")
		}
		telemetry.begin(telemetryMinor)
		telemetry.setPhase(telemetryPhaseTracing)
		telemetry.suspend()
		telemetry.resume()
		telemetry.noteRoot(RootGlobal)
		telemetry.addRootTime(RootGlobal, 3)
		if !telemetry.scanStart().IsZero() {
			panic("disabled scan clock changed")
		}
		telemetry.noteObjectScan(time.Time{}, 4, 5)
		telemetry.noteCardScan(time.Time{}, 6, 7, 8, 9, true)
		telemetry.noteNurseryOccupancy(10, 11)
		telemetry.noteSurvivor(12, 2, true, true)
		telemetry.notePromotion(13, true)
		telemetry.noteSweep(14)
		telemetry.end(true)
		telemetry.reset(ProfileThroughput, 15)
		c.beginCollectionTelemetry(telemetryFull)
		c.endCollectionTelemetry(true)
	}); allocs != 0 {
		t.Fatalf("disabled telemetry hook allocations = %v", allocs)
	}
	if *telemetry != (Telemetry{}) {
		t.Fatalf("disabled telemetry hooks mutated state: %+v", telemetry)
	}

	phase := PhaseTelemetry{TracingNS: 1}
	roots := RootTelemetry{NativeFrames: 2}
	trace := TraceTelemetry{ObjectsVisited: 3}
	nursery := NurseryTelemetry{SurvivedObjects: 4}
	cards := CardTelemetry{DirtyObjectCards: 5}
	before := []any{phase, roots, trace, nursery, cards}
	addPhaseTelemetry(&phase, PhaseTelemetry{TracingNS: 10})
	addRootTelemetry(&roots, RootTelemetry{NativeFrames: 10})
	addTraceTelemetry(&trace, TraceTelemetry{ObjectsVisited: 10})
	addNurseryTelemetry(&nursery, NurseryTelemetry{SurvivedObjects: 10})
	addCardTelemetry(&cards, CardTelemetry{DirtyObjectCards: 10})
	after := []any{phase, roots, trace, nursery, cards}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("disabled telemetry aggregation mutated values: before=%+v after=%+v", before, after)
	}
}

func TestCollectorTelemetryRequiresBuildTag(t *testing.T) {
	if TelemetryAvailable() {
		t.Fatal("ordinary release build reports collector telemetry available")
	}
	c, err := NewCollector(Config{Telemetry: new(Telemetry)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, ok := c.TelemetrySnapshot(); ok {
		t.Fatal("ordinary release build unexpectedly enabled collector telemetry")
	}
	if c.ResetTelemetry() {
		t.Fatal("ordinary release build unexpectedly reset collector telemetry")
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		if err := c.CollectMinor(nil); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Fatalf("disabled telemetry minor collection allocations = %v", allocs)
	}
	var out bytes.Buffer
	if err := NewBenchmarkTelemetryReport("disabled").WriteJSON(&out); err == nil || !strings.Contains(err.Error(), "wago_gcstats") {
		t.Fatalf("disabled JSON error = %v", err)
	}
}
