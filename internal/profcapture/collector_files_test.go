package profcapture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectorReturnsOriginalChildFailure(t *testing.T) {
	for _, collectorErr := range []error{nil, errors.New("collector killed")} {
		out := t.TempDir()
		if err := os.WriteFile(filepath.Join(out, "manifest.json"), []byte(`{"version":1,"complete":false,"diagnostics":["perf enable acknowledgement timed out"]}`), 0600); err != nil {
			t.Fatal(err)
		}
		err := ensureCollectorBundle(Options{Out: out}, collectorErr)
		if err == nil || !strings.Contains(err.Error(), "perf enable acknowledgement timed out") {
			t.Fatalf("child failure hidden by collector status: %v", err)
		}
		if collectorErr != nil && !errors.Is(err, collectorErr) {
			t.Fatalf("collector failure lost: %v", err)
		}
	}
}

func TestCollectorRequiresFinalManifest(t *testing.T) {
	for _, body := range []string{"", "{", `{"version":1,"complete":false}`, `{"version":1,"complete":true}`} {
		out := t.TempDir()
		if body != "" {
			if err := os.WriteFile(filepath.Join(out, "manifest.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
		err := ensureCollectorBundle(Options{Out: out}, nil)
		if (err == nil) != (body == `{"version":1,"complete":true}`) {
			t.Fatalf("manifest %q: %v", body, err)
		}
	}
}

func TestCollectorPreservesDamagedManifestAndPublishesFailure(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		exists     bool
	}{
		{name: "missing"}, {name: "empty", exists: true},
		{name: "truncated", body: `{"version":1,"complete":true,`, exists: true},
		{name: "unknown-version", body: `{"version":2,"complete":true}`, exists: true},
		{name: "contradictory-state", body: `{"version":1,"complete":true,"collector_pending":true,"backend":"perf"}`, exists: true},
		{name: "unexpected-pending-backend", body: `{"version":1,"collector_pending":true,"backend":"none"}`, exists: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := t.TempDir()
			if tt.exists {
				if err := os.WriteFile(filepath.Join(out, "manifest.json"), []byte(tt.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			collectorErr := errors.New("collector exited unexpectedly")
			err := ensureCollectorBundle(Options{Out: out, Backend: "perf", Phase: "execute", Mode: "public", SourceMaps: true, Rate: 99}, collectorErr)
			if err == nil || !errors.Is(err, collectorErr) {
				t.Fatalf("lost failure: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(out, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("failed bundle still unreadable: %v", err)
			}
			if m.Version != 1 || m.Complete || m.Backend != "perf" || m.RequestedRate != 99 || !m.SourceMapsRequested || m.SourceMaps || m.Iterations != 0 || len(m.Phases) != 0 || !strings.Contains(strings.Join(m.Diagnostics, " "), "unknown") {
				t.Fatalf("inaccurate failed manifest: %+v", m)
			}
			raw, err := os.ReadFile(filepath.Join(out, "manifest.child.invalid.json"))
			if tt.exists {
				if err != nil || string(raw) != tt.body {
					t.Fatalf("lost damaged child manifest: %q, %v", raw, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("invented original manifest: %v", err)
			}
		})
	}
}

func TestCollectorPublishesCompletionOnlyAfterFinalization(t *testing.T) {
	for _, collectorErr := range []error{nil, errors.New("conversion failed")} {
		out := t.TempDir()
		path := filepath.Join(out, "manifest.json")
		m := Manifest{Version: 1, Backend: "perf", CollectorPending: true, Iterations: 42, Diagnostics: []string{"coverage warning"}}
		if err := writeJSON(path, m); err != nil {
			t.Fatal(err)
		}
		if err := ensureCollectorBundle(Options{Out: out, Backend: "perf"}, nil); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var pending Manifest
		if err := json.Unmarshal(b, &pending); err != nil {
			t.Fatal(err)
		}
		if pending.Complete || !pending.CollectorPending {
			t.Fatal("bundle completed before conversion")
		}
		err = finishCollectorManifest(path, m, collectorErr)
		if !errors.Is(err, collectorErr) {
			t.Fatalf("lost conversion error: %v", err)
		}
		b, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var final Manifest
		if err := json.Unmarshal(b, &final); err != nil {
			t.Fatal(err)
		}
		if final.Complete != (collectorErr == nil) || final.CollectorPending || final.Iterations != 42 || final.Diagnostics[0] != "coverage warning" {
			t.Fatalf("incorrect final status: %+v", final)
		}
		if collectorErr != nil && !strings.Contains(strings.Join(final.Diagnostics, " "), collectorErr.Error()) {
			t.Fatal("lost conversion diagnostic")
		}
	}
}

func TestCollectorFailedManifestPublicationPreservesPendingState(t *testing.T) {
	out := t.TempDir()
	path := filepath.Join(out, "manifest.json")
	m := Manifest{Version: 1, Backend: "perf", CollectorPending: true}
	if err := writeJSON(path, m); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// An unencodable value prevents publication after temporary-file creation.
	m.Config = map[string]any{"bad": make(chan int)}
	if err := finishCollectorManifest(path, m, nil); err == nil {
		t.Fatal("encoding failure ignored")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) {
		t.Fatalf("lost pending state: %q, %v", after, err)
	}
	files, err := filepath.Glob(filepath.Join(out, ".manifest-*.json"))
	if err != nil || len(files) != 0 {
		t.Fatalf("left incomplete publication files: %v, %v", files, err)
	}
}

func TestCollectorCannotPromoteFailedWorkload(t *testing.T) {
	out := t.TempDir()
	path := filepath.Join(out, "manifest.json")
	m := Manifest{Version: 1, Backend: "perf", Diagnostics: []string{"guest trapped"}}
	if err := writeJSON(path, m); err != nil {
		t.Fatal(err)
	}
	if err := finishCollectorManifest(path, m, nil); err == nil {
		t.Fatal("failed workload promoted to success")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Complete || m.CollectorPending || m.Diagnostics[0] != "guest trapped" {
		t.Fatalf("lost workload failure: %+v", m)
	}
}

func TestCollectorDoesNotOverwriteExistingFailureEvidence(t *testing.T) {
	out := t.TempDir()
	for name, body := range map[string]string{"manifest.json": "{", "manifest.child.invalid.json": "earlier evidence"} {
		if err := os.WriteFile(filepath.Join(out, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureCollectorBundle(Options{Out: out}, nil); err == nil {
		t.Fatal("evidence collision ignored")
	}
	for name, want := range map[string]string{"manifest.json": "{", "manifest.child.invalid.json": "earlier evidence"} {
		got, err := os.ReadFile(filepath.Join(out, name))
		if err != nil || string(got) != want {
			t.Fatalf("overwrote %s: %q, %v", name, got, err)
		}
	}
}
