package wago

import (
	"reflect"
	"testing"
)

func TestDomainSnapshotV4PersistsSurvivorPolicy(t *testing.T) {
	cfg := GCConfig{
		Profile: GCProfileThroughput, NurseryBytes: 1024, SurvivorBytes: 384,
		MinorPauseTargetMicros: 750, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096,
		CollectEveryAlloc: true, VerifyAfterCollect: true,
	}
	blob := appendDomainGCConfig(nil, cfg, 4)
	rd := &snapReader{buf: blob}
	got := readDomainGCConfig(rd, 4)
	if rd.err != nil || len(rd.buf) != 0 {
		t.Fatalf("decode state err/remaining=%v/%d want nil/0", rd.err, len(rd.buf))
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("decoded survivor config = %+v, want %+v", got, cfg)
	}
	legacy := appendDomainGCConfig(nil, cfg, 3)
	legacyRD := &snapReader{buf: legacy}
	legacyGot := readDomainGCConfig(legacyRD, 3)
	if legacyRD.err != nil || legacyGot.SurvivorBytes != 0 || legacyGot.MinorPauseTargetMicros != 0 {
		t.Fatalf("legacy survivor config = %+v, err=%v", legacyGot, legacyRD.err)
	}
}
