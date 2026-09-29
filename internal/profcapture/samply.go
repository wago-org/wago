package profcapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RecordSamply preserves Samply's raw profile. Its samples may include off-CPU
// observations; this backend makes no CPU-time or precise phase-isolation claim.
func RecordSamply(o Options, args []string) error {
	if _, err := os.Stat(o.Out); !os.IsNotExist(err) {
		return fmt.Errorf("output must not already exist: %s", o.Out)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "wagoprof-samply-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "samply.json.gz")
	flags := []string{"record", "--save-only", "--rate", fmt.Sprint(o.Rate), "--output", path, "--", exe}
	flags = append(flags, o.CommandPrefix...)
	flags = append(flags, "capture")
	flags = append(flags, args...)
	ctx, cancel := timedContext(o.collectionTimeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, o.Samply, flags...)
	boundProcess(cmd)
	cmd.Stdout = os.Stdout
	log, err := os.Create(filepath.Join(dir, "collector.log"))
	if err != nil {
		return err
	}
	cmd.Stderr = log
	runErr := errors.Join(cmd.Run(), ctx.Err(), log.Close())
	runErr = ensureCollectorBundle(o, runErr)
	for _, name := range []string{"samply.json.gz", "collector.log"} {
		if err := copyCaptureFile(filepath.Join(dir, name), filepath.Join(o.Out, name)); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}
	manifest := filepath.Join(o.Out, "manifest.json")
	b, err := os.ReadFile(manifest)
	if err != nil {
		return errors.Join(runErr, err)
	}
	var m Manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return errors.Join(runErr, err)
	}
	return finishCollectorManifest(manifest, m, runErr)
}
