// jsonprof is the compatibility preset for wagoprof's JSON-AS workload. New
// captures should use wagoprof record --workload json-as. External sampling can
// use `perf record -F 499 -- ./jsonprof 15s`; precise phase isolation requires
// wagoprof's perf backend and its start/stop acknowledgement protocol.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wago-org/wago/internal/profcapture"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "jsonprof:", err)
		os.Exit(1)
	}
}
func run(args []string) error {
	duration := 15 * time.Second
	if len(args) > 2 {
		return fmt.Errorf("usage: jsonprof [positive-duration] [guard]")
	}
	if len(args) > 0 {
		var err error
		duration, err = time.ParseDuration(args[0])
		if err != nil || duration <= 0 {
			return fmt.Errorf("invalid positive duration %q", args[0])
		}
	}
	bounds := "explicit"
	if len(args) == 2 {
		if args[1] != "guard" {
			return fmt.Errorf("unknown bounds argument %q", args[1])
		}
		bounds = "signals"
	}
	w, b, err := profcapture.LoadWorkload("../corpus/catalog.json", "json-as", "", "", "", "", "")
	if err != nil {
		return err
	}
	if module := os.Getenv("WAGO_JSON_MODULE"); module != "" {
		b, err = os.ReadFile(module)
		if err != nil {
			return err
		}
		w.Artifact = module
		sum := sha256.Sum256(b)
		w.Hash = hex.EncodeToString(sum[:])
	}
	switch only := os.Getenv("WAGO_JSONPROF_ONLY"); only {
	case "":
	case "ser":
		w.Calls = w.Calls[:1]
	case "deser":
		w.Calls = w.Calls[1:]
	default:
		return fmt.Errorf("invalid WAGO_JSONPROF_ONLY %q", only)
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("jsonprof-%d.wagoprof", os.Getpid()))
	fmt.Printf("jsonprof pid=%d, bundle=%s (external capture includes all phases)\n", os.Getpid(), out)
	return profcapture.Run(profcapture.Options{Out: out, Backend: "perf-map", Phase: "execute", Mode: "public", Duration: duration, Warmup: 5, Bounds: bounds, Rate: 499}, w, b)
}

func modulePath() string {
	if path := os.Getenv("WAGO_JSON_MODULE"); path != "" {
		return path
	}
	return "../corpus/workloads/assemblyscript/json-as.wasm"
}
