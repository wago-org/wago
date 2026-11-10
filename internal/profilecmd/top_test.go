//go:build wago_profile

package profilecmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/internal/profcapture"
	"github.com/wago-org/wago/profile"
)

func TestInitializationCostsIgnoreLaterExecutionCount(t *testing.T) {
	c := capture{manifest: profcapture.Manifest{Backend: "perf", Phase: "initialize", PhaseIsolation: "collector-start-stop", Phases: []profcapture.Phase{{Name: "initialize", Elapsed: 200, Completed: 1, WorkUnit: "initialization"}}}, report: profile.Report{Unit: "nanoseconds", Rows: []profile.Row{{Weight: 100}}}}
	var before string
	for _, iterations := range []uint64{100, 100000} {
		c.manifest.Iterations = iterations
		r, err := makeTop(c, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := writeTop(&out, r, false); err != nil {
			t.Fatal(err)
		}
		if r.Completed != 1 || *r.Rows[0].CPUPerWork != 100 || !strings.Contains(out.String(), "CPU ns/initialization") {
			t.Fatal(out.String())
		}
		if before != "" && before != out.String() {
			t.Fatal("initialization changed with later execution work")
		}
		before = out.String()
	}
	c.manifest.Phases = nil
	r, err := makeTop(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.CPUPerWork || r.Rows[0].CPUPerWork != nil {
		t.Fatal("invented phase denominator for old capture")
	}
	c.manifest.Phase = "all"
	r, err = makeTop(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.CPUPerWork {
		t.Fatal("whole capture mislabeled per iteration")
	}
}
func TestTopJSONAndTextShareStaticRowsAndLimit(t *testing.T) {
	c := capture{manifest: profcapture.Manifest{Backend: "none", Phase: "execute", Iterations: 10}, events: []wago.CodeProfileEvent{{Image: &wago.CodeProfileImage{ModuleID: "m", ArtifactID: "a", Functions: []wago.CodeProfileFunction{{Index: 0, Name: "first", NativeBytes: 42}, {Index: 1, Name: "second", NativeBytes: 80}}}}}}
	for _, backend := range []string{"none", "perf", "samply"} {
		c.manifest.Backend = backend
		c.report.Rows = []profile.Row{{Name: "first", Weight: 42}, {Name: "second", Weight: 80}}
		if backend == "none" {
			c.report = profile.Report{}
		}
		r, err := makeTop(c, 1)
		if err != nil {
			t.Fatal(err)
		}
		var js, text bytes.Buffer
		if err := writeTop(&js, r, true); err != nil {
			t.Fatal(err)
		}
		if err := writeTop(&text, r, false); err != nil {
			t.Fatal(err)
		}
		var decoded topReport
		if err := json.Unmarshal(js.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Rows) != 1 || !strings.Contains(text.String(), "first") || strings.Contains(text.String(), "second") {
			t.Fatalf("views diverge: %s / %s", js.String(), text.String())
		}
		if backend == "none" && (decoded.Rows[0].Static == nil || decoded.Rows[0].Static.NativeBytes != 42) {
			t.Fatal("static JSON lost compiler metadata")
		}
	}
}
