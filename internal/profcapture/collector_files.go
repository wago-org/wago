package profcapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func copyCaptureFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}
func ensureCollectorBundle(o Options, runErr error) error {
	if _, err := os.Stat(o.Out); os.IsNotExist(err) {
		if err := os.Mkdir(o.Out, 0700); err != nil {
			return errors.Join(runErr, err)
		}
	} else if err != nil {
		return errors.Join(runErr, err)
	}
	path := filepath.Join(o.Out, "manifest.json")
	data, readErr := os.ReadFile(path)
	var m Manifest
	manifestErr := readErr
	if readErr == nil {
		manifestErr = json.Unmarshal(data, &m)
		if manifestErr == nil && m.Version != 1 {
			manifestErr = fmt.Errorf("unsupported capture manifest version %d", m.Version)
		}
		if manifestErr == nil && m.CollectorPending && (m.Complete || m.Backend != o.Backend || m.Backend != "perf" && m.Backend != "samply" && !m.ParentSupervised) {
			manifestErr = fmt.Errorf("invalid pending collector state")
		}
	} else if !os.IsNotExist(readErr) {
		// A permission or I/O error is not permission to replace evidence.
		return errors.Join(runErr, fmt.Errorf("read final capture manifest: %w", readErr))
	}
	if manifestErr == nil {
		if !m.Complete && !m.CollectorPending {
			// Collector termination may only be a consequence of the workload
			// failure. Return the original diagnostic to the CLI as well as
			// retaining it in the bundle; do not proceed to normal conversion.
			detail := strings.Join(m.Diagnostics, "; ")
			if detail == "" {
				detail = "child did not complete"
			}
			return errors.Join(fmt.Errorf("workload capture failed: %s", detail), runErr)
		}
		return runErr
	}
	runErr = errors.Join(runErr, fmt.Errorf("read final capture manifest: %w", manifestErr))
	diagnostics := []string{"workload manifest unavailable; actual phases and completed work are unknown; see collector.log", runErr.Error()}
	if readErr == nil {
		// Keep the exact original bytes, including partial JSON, before
		// publishing a schema-valid failure. Exclusive creation protects any
		// earlier evidence; an unsuccessful copy must leave the original alone.
		const backup = "manifest.child.invalid.json"
		if err := copyCaptureFile(path, filepath.Join(o.Out, backup)); err != nil {
			return errors.Join(runErr, fmt.Errorf("preserve child manifest: %w", err))
		}
		if err := os.Remove(path); err != nil {
			return errors.Join(runErr, err)
		}
		diagnostics = append(diagnostics, "original child manifest retained as "+backup)
	}
	m = Manifest{Revision: BuildRevision, Dirty: BuildDirty, GoVersion: runtime.Version(), CPUs: runtime.NumCPU(), RequestedRate: o.Rate, RequestedNS: int64(o.Duration), Warmup: o.Warmup, CodeIncluded: o.IncludeCode, Version: 1, Complete: false, Backend: o.Backend, Target: runtime.GOOS + "/" + runtime.GOARCH, Phase: o.Phase, Mode: o.Mode, Diagnostics: diagnostics}
	if m.Revision == "" {
		m.Revision = "unknown"
	}
	if m.Dirty == "" {
		m.Dirty = "unknown"
	}
	m.CollectionTimeoutNS, m.ConversionTimeoutNS = int64(o.collectionTimeout()), int64(o.conversionTimeout())
	m.RawStackBytes = o.RawStackBytes
	m.UnwindMapsRequested = o.UnwindMaps
	m.SourceMapsRequested = o.SourceMaps
	m.ReloadArtifact = o.ReloadArtifact || o.Phase == "reload"
	m.PhaseIsolation = "unknown"
	m.CPUModel, m.OSVersion = hostIdentity()
	return errors.Join(runErr, writeJSON(path, m))
}

// finishCollectorManifest publishes completion only after all collector work
// succeeds. If encoding, closing, or replacement fails, the old pending/failed
// manifest remains authoritative. Readers never see a partially rewritten JSON
// file on the supported Linux/macOS collector targets.
func finishCollectorManifest(path string, m Manifest, runErr error) error {
	if runErr == nil && !m.Complete && !m.CollectorPending {
		runErr = fmt.Errorf("cannot complete capture: workload did not finish successfully")
	}
	m.CollectorPending = false
	m.Complete = runErr == nil
	if runErr != nil {
		m.Diagnostics = append(m.Diagnostics, runErr.Error())
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.json")
	if err != nil {
		return errors.Join(runErr, err)
	}
	defer os.Remove(f.Name())
	e := json.NewEncoder(f)
	e.SetIndent("", "  ")
	err = errors.Join(e.Encode(m), f.Close())
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return errors.Join(runErr, err)
}
