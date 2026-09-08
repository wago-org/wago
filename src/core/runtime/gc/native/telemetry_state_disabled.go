//go:build !wago_gcstats

package gc

import (
	"runtime"
	"time"
)

// Telemetry is an inert marker in ordinary release builds. Full bounded state
// and timing are compiled only with wago_gcstats.
type Telemetry struct {
	active                  telemetryCycle
	paths                   PathTelemetry
	barriers                BarrierTelemetry
	pendingDuplicateDirties uint64
	allocationBaseline      uint64
}

type telemetryCycleKind uint8

const (
	telemetryMinor telemetryCycleKind = iota + 1
	telemetryFull
)

type telemetryPhase uint8

const (
	telemetryPhaseNone telemetryPhase = iota
	telemetryPhaseRootEnumeration
	telemetryPhasePersistentRoots
	telemetryPhaseNativeRoots
	telemetryPhaseRememberedRoots
	telemetryPhaseTracing
	telemetryPhaseMarking
	telemetryPhasePromotionCopy
	telemetryPhaseSweep
	telemetryPhaseFreeSpaceReconstruction
	telemetryPhaseFragmentationRecovery
	telemetryPhaseMetadataCleanup
	telemetryPhaseCount
)

type telemetryCycle struct {
	active         bool
	phase          telemetryPhase
	rememberedScan bool
	cards          CardTelemetry
}

func (*Telemetry) attach(Profile, uint64)                                         {}
func (*Telemetry) setGlobalRootClass(uint32, RootClass)                           {}
func (*Telemetry) globalRootClass(uint32) RootClass                               { return RootGlobal }
func (*Telemetry) begin(telemetryCycleKind)                                       {}
func (*Telemetry) end(bool)                                                       {}
func (*Telemetry) setPhase(telemetryPhase)                                        {}
func (*Telemetry) suspend()                                                       {}
func (*Telemetry) resume()                                                        {}
func (*Telemetry) noteRoot(RootClass)                                             {}
func (*Telemetry) addRootTime(RootClass, uint64)                                  {}
func (*Telemetry) scanStart() time.Time                                           { return time.Time{} }
func (*Telemetry) noteObjectScan(time.Time, uint32, uint32)                       {}
func (*Telemetry) noteObjectScanWork(time.Time, objectScanWork, bool, bool, bool) {}
func (*Telemetry) noteTinyStepWork(objectScanWork)                                {}
func (*Telemetry) noteCardScan(time.Time, uint64, uint64, uint64, uint64, bool)   {}
func (*Telemetry) noteNurseryOccupancy(uint64, uint64)                            {}
func (*Telemetry) noteSurvivor(uint32, uint8, bool, bool)                         {}
func (*Telemetry) notePromotion(uint32, bool)                                     {}
func (*Telemetry) noteSweep(uint32)                                               {}
func (*Telemetry) reset(Profile, uint64)                                          {}

func (c *Collector) beginCollectionTelemetry(telemetryCycleKind) {}
func (c *Collector) endCollectionTelemetry(bool)                 {}

func (c *Collector) TelemetrySnapshot() (TelemetrySnapshot, bool) {
	return TelemetrySnapshot{}, false
}

func (c *Collector) ResetTelemetry() bool { return false }

func addPhaseTelemetry(*PhaseTelemetry, PhaseTelemetry)       {}
func addRootTelemetry(*RootTelemetry, RootTelemetry)          {}
func addTraceTelemetry(*TraceTelemetry, TraceTelemetry)       {}
func addNurseryTelemetry(*NurseryTelemetry, NurseryTelemetry) {}
func addCardTelemetry(*CardTelemetry, CardTelemetry)          {}

func CaptureMemoryDomains(compilerHeapBytes, executableJITBytes uint64, heap ManagedHeapTelemetry) MemoryDomains {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return MemoryDomains{
		GoCompilerHeapBytes: compilerHeapBytes,
		GoRuntimeHeapBytes:  mem.HeapAlloc,
		WasmManagedBytes:    heap.CommittedBytes,
		ExecutableJITBytes:  executableJITBytes,
		PeakRSSBytes:        peakRSSBytes(),
	}
}
