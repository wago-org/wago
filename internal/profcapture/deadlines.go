package profcapture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func timedContext(d time.Duration) (context.Context, context.CancelFunc) {
	parent, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithTimeout(parent, d)
	return ctx, func() { cancel(); stop() }
}

const DefaultCollectionTimeout = 5 * time.Minute
const DefaultConversionTimeout = 2 * time.Minute

func (o Options) collectionTimeout() time.Duration {
	if o.CollectionTimeout > 0 {
		return o.CollectionTimeout
	}
	return DefaultCollectionTimeout
}
func (o Options) conversionTimeout() time.Duration {
	if o.ConversionTimeout > 0 {
		return o.ConversionTimeout
	}
	return DefaultConversionTimeout
}

// RecordLocal supervises the workload in a separate process. The safety deadline
// can stop a non-returning guest call without changing the measured invocation API.
func RecordLocal(o Options, args []string) error {
	if _, err := os.Stat(o.Out); !os.IsNotExist(err) {
		return fmt.Errorf("output must not already exist: %s", o.Out)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "wagoprof-local-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	log, err := os.Create(filepath.Join(dir, "collector.log"))
	if err != nil {
		return err
	}
	ctx, cancel := timedContext(o.collectionTimeout())
	defer cancel()
	flags := append([]string(nil), o.CommandPrefix...)
	flags = append(flags, "capture")
	flags = append(flags, args...)
	flags = append(flags, "--supervised")
	cmd := exec.CommandContext(ctx, exe, flags...)
	boundProcess(cmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = log
	runErr := errors.Join(cmd.Run(), ctx.Err(), log.Close())
	runErr = ensureCollectorBundle(o, runErr)
	runErr = errors.Join(runErr, copyCaptureFile(filepath.Join(dir, "collector.log"), filepath.Join(o.Out, "collector.log")))
	b, err := os.ReadFile(filepath.Join(o.Out, "manifest.json"))
	if err != nil {
		return errors.Join(runErr, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return errors.Join(runErr, err)
	}
	return finishCollectorManifest(filepath.Join(o.Out, "manifest.json"), m, runErr)
}
