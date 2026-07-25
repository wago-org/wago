package wasmtimetest

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunIsolatedRoundTrip(t *testing.T) {
	if RunIsolated(t, 5*time.Second) {
		return
	}
	if os.Getenv(TargetEnv) != t.Name() {
		t.Fatalf("selected child target = %q, want %q", os.Getenv(TargetEnv), t.Name())
	}
}

func TestCaptureBoundsOutputAndPreservesMarkers(t *testing.T) {
	capture := NewCapture(32, OutcomeMark)
	_, _ = capture.Write([]byte(strings.Repeat("a", 100) + "\n"))
	_, _ = capture.Write([]byte(OutcomeMark + `{"nonce":"one"}` + "\n"))
	_, _ = capture.Write([]byte(OutcomeMark + `{"nonce":"two"}` + "\n"))
	_, _ = capture.Write([]byte(strings.Repeat("z", 100)))
	out := capture.Output()
	if len(out) > 256 || !strings.Contains(out, "truncated") || !strings.HasPrefix(out, strings.Repeat("a", 16)) || !strings.HasSuffix(out, strings.Repeat("z", 16)) {
		t.Fatalf("bounded output = %q", out)
	}
	markers := capture.Markers()
	if len(markers) != 2 || !strings.Contains(markers[0], "one") || !strings.Contains(markers[1], "two") {
		t.Fatalf("markers = %q", markers)
	}
}

func TestCaptureRetainsSmallOutputExactly(t *testing.T) {
	capture := NewCapture(64, "marker=")
	want := "small output\n"
	_, _ = capture.Write([]byte(want))
	if got := capture.Output(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestChildEnvironmentStripsInheritedWagoKnobs(t *testing.T) {
	t.Setenv("WAGO_INLINE", "0")
	t.Setenv("WAGO_BOUNDS", "stale")
	t.Setenv("WAGO_WASMTIME_TIMEOUT", "99s")
	env := ChildEnvironment(map[string]string{"WAGO_BOUNDS": ExpectedBounds, TargetEnv: "target"})
	values := map[string][]string{}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "WAGO_") {
			values[key] = append(values[key], value)
		}
	}
	if len(values) != 2 || len(values["WAGO_BOUNDS"]) != 1 || values["WAGO_BOUNDS"][0] != ExpectedBounds || len(values[TargetEnv]) != 1 {
		t.Fatalf("sanitized WAGO environment = %v; process env has %d entries", values, len(os.Environ()))
	}
}

func TestChildEnvironmentCanExplicitlyPreserveOptimizationKnobs(t *testing.T) {
	t.Setenv("WAGO_WASMTIME_PRESERVE_KNOBS", "1")
	t.Setenv("WAGO_INLINE", "0")
	t.Setenv("WAGO_BOUNDS", "stale")
	env := ChildEnvironment(map[string]string{"WAGO_BOUNDS": ExpectedBounds})
	values := map[string][]string{}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "WAGO_") {
			values[key] = append(values[key], value)
		}
	}
	if len(values["WAGO_INLINE"]) != 1 || values["WAGO_INLINE"][0] != "0" || len(values["WAGO_BOUNDS"]) != 1 || values["WAGO_BOUNDS"][0] != ExpectedBounds {
		t.Fatalf("preserved WAGO environment = %v", values)
	}
}

func TestValidateOutcomeRejectsMissingDuplicateSkippedAndMismatchedTargets(t *testing.T) {
	valid := `{"protocol":"2","target":"target","nonce":"nonce","status":"pass"}`
	if err := ValidateOutcome([]string{valid}, "target", "nonce"); err != nil {
		t.Fatal(err)
	}
	for _, markers := range [][]string{
		nil,
		{valid, valid},
		{`{"protocol":"2","target":"target","nonce":"nonce","status":"skip"}`},
		{`{"protocol":"2","target":"other","nonce":"nonce","status":"pass"}`},
	} {
		if err := ValidateOutcome(markers, "target", "nonce"); err == nil {
			t.Fatalf("invalid outcomes were accepted: %q", markers)
		}
	}
}

func TestOutcomeJSONIsStrict(t *testing.T) {
	var outcome Outcome
	if err := DecodeStrictJSON([]byte(`{"protocol":"2","target":"t","nonce":"n","status":"pass"}`), &outcome); err != nil {
		t.Fatal(err)
	}
	if err := DecodeStrictJSON([]byte(`{"protocol":"2","target":"t","nonce":"n","status":"pass","extra":true}`), &outcome); err == nil {
		t.Fatal("unknown outcome field was accepted")
	}
	if err := DecodeStrictJSON([]byte(`{"protocol":"2","target":"t","nonce":"n","status":"pass"}{}`), &outcome); err == nil {
		t.Fatal("multiple outcome values were accepted")
	}
}
