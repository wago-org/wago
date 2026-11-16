//go:build linux && amd64 && wago_profile && wago_gc_direct_trusted_alloc

package wago

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

// TestGCNativeProfileProbe captures metadata, not native samples or timing.
// It preserves the direct bridge's seven-tag policy and disabled allocation elision.
func TestGCNativeProfileProbe(t *testing.T) {
	out := os.Getenv("GC_NATIVE_PROFILE_OUT")
	if out == "" {
		t.Skip("set GC_NATIVE_PROFILE_OUT to a new output directory")
	}
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../bench/experiments/gc-v8/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, File, Export, SHA256 string
		N, Want                    uint64
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	names := []string{"struct-allocation-access", "array-allocation-access", "array-fill", "array-copy-checksum", "array-loop-copy-checksum", "tree-survival"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			var data []byte
			var export, digest string
			var n, want uint64
			for _, c := range cases {
				if c.Name == name {
					data, err = os.ReadFile(filepath.Join("../../bench/experiments/gc-v8", c.File))
					if err != nil {
						t.Fatal(err)
					}
					export, digest, n, want = c.Export, c.SHA256, c.N, c.Want
				}
			}
			if len(data) == 0 || fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
				t.Fatal("artifact mismatch")
			}
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false)
			plainRT := NewRuntime(WithRuntimeConfig(cfg))
			defer plainRT.Close()
			plain, err := plainRT.Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer plain.Close()
			session := NewCodeProfile(CodeProfileOptions{IncludeCode: true, SourceMaps: true})
			defer session.Close()
			rt := NewRuntime(WithRuntimeConfig(cfg.WithCodeProfile(session)))
			defer rt.Close()
			observed, err := rt.Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer observed.Close()
			if !bytes.Equal(plain.code, observed.code) {
				t.Fatal("profiling changed emitted native code")
			}
			config := GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 2 << 20, ThroughputHeapBytes: 8 << 20}
			a, err := plainRT.Instantiate(context.Background(), plain, WithGC(config))
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := rt.Instantiate(context.Background(), observed, WithGC(config))
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			for _, in := range []*Instance{a, b} {
				if in.gcDirectView == nil || !in.gc.UsesDirectOffsets() {
					t.Fatal("direct native collector absent")
				}
				view := in.gc.DirectView()
				if view == nil || view.NurseryEnd-view.NurseryStart != 2<<20 || view.ArenaBytes != 16+(18<<20) {
					t.Fatal("collector geometry mismatch")
				}
				if in.c.codeCache == nil || !bytes.Equal(in.c.codeCache.mem[:len(in.c.code)], in.c.code) {
					t.Fatal("loaded native bytes differ")
				}
				for i := 0; i < 128; i++ {
					got, e := in.Invoke(export, n)
					if e != nil || len(got) != 1 || got[0] != want {
						t.Fatal("checksum", got, e)
					}
				}
			}
			plainStats, profileStats := a.gc.Stats(), b.gc.Stats()
			if plainStats != profileStats || profileStats.MinorCollections+profileStats.FullCollections == 0 {
				t.Fatal("collector accounting changed or no collections", plainStats, profileStats)
			}
			events, status := session.Read(0)
			if status.Dropped != 0 || len(events) == 0 {
				t.Fatal("missing/lost metadata", status)
			}
			var mapped, total uint64
			var images int
			for _, event := range events {
				if event.Kind != "load" || event.Image == nil {
					t.Fatal("unexpected pre-close event", event.Kind)
				}
				im := event.Image
				images++
				if im.Size != uint64(len(im.Code)) {
					t.Fatal("code image extent mismatch")
				}
				if err := jitprofile.ValidateRegions(im.Regions, im.Size); err != nil {
					t.Fatal(err)
				}
				if err := jitprofile.ValidateSources(im.Sources, im.Size); err != nil {
					t.Fatal(err)
				}
				if err := jitprofile.ValidateInlineSources(im.Sources, im.InlineFrames); err != nil {
					t.Fatal(err)
				}
				if err := jitprofile.ValidateCodeSites(im.CodeSites, im.Size); err != nil {
					t.Fatal(err)
				}
				if err := jitprofile.ValidateCodeSiteRegions(im.CodeSites, im.Regions); err != nil {
					t.Fatal(err)
				}
				total += im.Size
				for _, r := range im.Sources {
					mapped += r.Size
				}
			}
			// Retirement must follow the matching load and preserve code for offline use.
			b.Close()
			observed.Close()
			rt.Close()
			events, status = session.Read(0)
			live := map[uint64]uint64{}
			var seq, stamp uint64
			for _, event := range events {
				if event.Sequence <= seq || event.Timestamp < stamp {
					t.Fatal("nonmonotonic image journal")
				}
				seq, stamp = event.Sequence, event.Timestamp
				switch event.Kind {
				case "load":
					if _, ok := live[event.ImageID]; ok {
						t.Fatal("duplicate load")
					}
					live[event.ImageID] = event.Timestamp
				case "retire":
					if _, ok := live[event.ImageID]; !ok {
						t.Fatal("retire without load")
					}
					delete(live, event.ImageID)
				default:
					t.Fatal("unexpected event", event.Kind)
				}
			}
			if len(live) != 0 || status.Dropped != 0 {
				t.Fatal("unretired/lost images", live, status)
			}
			receipt := map[string]any{"case": name, "artifact_sha256": digest, "native_sha256": fmt.Sprintf("%x", sha256.Sum256(plain.code)), "native_bytes": len(plain.code), "exact_native_parity": true, "loaded_byte_parity": true, "checksum": want, "calls_per_mode": 128, "config": config, "plain_stats": plainStats, "profile_stats": profileStats, "allocation_elision": false, "image_count": images, "mapped_native_bytes": mapped, "total_image_bytes": total, "unmapped_native_bytes": total - mapped, "status": status, "events": events, "samples_collected": false}
			encoded, err := json.MarshalIndent(receipt, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(out, name+".json"), encoded, 0644); err != nil {
				t.Fatal(err)
			}
			t.Logf("NATIVE_PROFILE_RESULT case=%s exact_parity=true native_bytes=%d mapped=%d total=%d samples=false", name, len(plain.code), mapped, total)
		})
	}
}
