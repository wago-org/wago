//go:build amd64

package amd64

import (
	"os"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func readRegionFixture(t *testing.T) (*wasm.Module, int) {
	t.Helper()
	data, err := os.ReadFile("../../../../../../tests/fixtures/wasm/gc-read-regions.wasm")
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	for _, e := range m.Exports {
		if e.Name == "int-read" {
			return m, int(e.Index.Index) - m.ImportedFuncCount()
		}
	}
	t.Fatal("missing int-read")
	return nil, 0
}

func TestGCReadRegionAdmissionBoundary(t *testing.T) {
	for _, name := range []string{"accept", "arrays-disabled", "allocation", "store", "loop", "host-call", "array-len", "numeric-shape", "numeric-signature", "declared-local", "float-parameter", "large", "open-type", "all-trap"} {
		t.Run(name, func(t *testing.T) {
			m, local := readRegionFixture(t)
			arrays := true
			switch name {
			case "arrays-disabled":
				arrays = false
			case "allocation":
				m.Code[local].BodyBytes = append([]byte{0xfb, 1, 4, 0x1a}, m.Code[local].BodyBytes...)
			case "store":
				m.Code[local].BodyBytes = append([]byte{0x24, 0}, m.Code[local].BodyBytes...)
			case "loop":
				m.Code[local].BodyBytes = append([]byte{0x03, 0x40, 0x0b}, m.Code[local].BodyBytes...)
			case "host-call":
				m.Code[local].BodyBytes = append([]byte{0x10, 5}, m.Code[local].BodyBytes...)
			case "array-len":
				m.Code[local].BodyBytes = []byte{0x20, 0, 0xfb, 22, 0, 0xfb, 15, 0x0b}
			case "numeric-shape":
				m.Code[0].BodyBytes = []byte{0x20, 0, 0x20, 1, 0x4f, 0x41, 0, 0x6a, 0x0b}
			case "numeric-signature":
				ft, _ := m.LocalFuncType(0)
				ft.Params[0] = wasm.I64
			case "declared-local":
				m.Code[local].Locals.Runs = append(m.Code[local].Locals.Runs, wasm.LocalRun{Count: 1, Type: wasm.I32})
			case "float-parameter":
				ft, _ := m.LocalFuncType(local)
				ft.Params[0] = wasm.F64
			case "large":
				for len(m.Code[local].BodyBytes) <= 192 {
					m.Code[local].BodyBytes = append([]byte{1}, m.Code[local].BodyBytes...)
				}
			case "all-trap":
				m.Code[local].BodyBytes = []byte{0x20, 0, 0xfb, 22, 2, 0xfb, 2, 2, 1, 0x04, 0x7f, 0, 5, 0, 0x0b, 0x0b}
			case "open-type":
				m.Types[2].SubTypes[0].Final = false
			}
			p := planGCReadRegion(m, local, nil, arrays)
			if (p != nil) != (name == "accept") {
				t.Fatalf("plan=%+v", p)
			}
		})
	}
}

func TestGCReadRegionNativeOnlyGuard(t *testing.T) {
	s := gcReadRegionState{}
	f := fn{gcReadRegion: &s}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"GC helper", func() error { return f.callGCStructHelper(1, nil, nil) }},
		{"host", func() error { return f.callHost(0, nil) }},
		{"sync host", func() error { return f.callHostSync(0, nil) }},
		{"internal", func() error { return f.callInternal(0, nil, -1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || !strings.Contains(err.Error(), "native-only GC read region") {
				t.Fatalf("guard=%v", err)
			}
		})
	}
	f.recordGCFrameSafepoint(0)
	if s.err == nil {
		t.Fatal("safepoint fallback did not report its invariant failure")
	}
	s.err = nil
	f.consumeGCFrameCallsite()
	if s.err == nil {
		t.Fatal("source mask consumption did not report its invariant failure")
	}
	s.err = nil
	f.prepareGCFrameCallsite(0)
	if s.err == nil {
		t.Fatal("source mask preparation did not report its invariant failure")
	}
}

func TestGCReadRegionAddressExcludesMergeRegister(t *testing.T) {
	f := fn{regMerge: true}
	if got := f.gcReadRegionRegister(); got == mergeReg {
		t.Fatalf("raw object address cannot use active branch-result register %d", got)
	}
	f.reserved = maskOf(RBP, R12, R13, R14)
	if got := f.gcReadRegionRegister(); got != regNone {
		t.Fatalf("no free address register: got %d", got)
	}
}

// A large caller may mention many read targets, but the optimization must not
// reserve an unbounded local frame for calls beyond its emission budget.
func TestGCReadRegionCallerReservationBound(t *testing.T) {
	data := &inlineTargetData{slots: make([]uint32, 40)}
	var body []byte
	for i := range data.slots {
		data.slots[i] = uint32(i + 1)
		data.targets = append(data.targets, inlineTarget{globalIdx: i, readRegion: &gcReadRegionPlan{}})
		body = append(body, 0x10, byte(i))
	}
	body = append(body, 0x0b)
	plan := buildInlineCallerPlan(&wasm.Func{BodyBytes: body}, inlineTargetTable{data: data})
	if len(plan.callees) != 16 || plan.allCallsInline {
		t.Fatalf("reserved=%d allCallsInline=%v", len(plan.callees), plan.allCallsInline)
	}
}
