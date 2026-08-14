package run

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"time"

	"github.com/wago-org/wago/cli/internal/handoff"
	"github.com/wago-org/wago/cli/internal/ui"
)

const watchStopGrace = 2 * time.Second

func watchModule(path, intervalValue string) {
	interval := 200 * time.Millisecond
	if intervalValue != "" {
		parsed, err := time.ParseDuration(intervalValue)
		if err != nil || parsed <= 0 {
			ui.Usage("run: --watch-interval must be a positive duration")
		}
		interval = parsed
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, watchedSignals()...)
	defer signal.Stop(interrupts)
	err := superviseWatch(context.Background(), watchOptions{
		path:       path,
		interval:   interval,
		debounce:   interval,
		stopGrace:  watchStopGrace,
		executable: os.Args[0],
		arguments:  withoutWatchFlags(os.Args[1:]),
		stdin:      os.Stdin,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		interrupts: interrupts,
	})
	var interrupted *watchInterruptedError
	if errors.As(err, &interrupted) {
		os.Exit(watchedSignalExitCode(interrupted.signal))
	}
	if err != nil {
		ui.Fatal("watch: %v", err)
	}
}

type watchOptions struct {
	path        string
	interval    time.Duration
	debounce    time.Duration
	stopGrace   time.Duration
	executable  string
	arguments   []string
	environment []string
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	interrupts  <-chan os.Signal
}

type watchInterruptedError struct {
	signal os.Signal
}

func (err *watchInterruptedError) Error() string { return "interrupted by " + err.signal.String() }

func superviseWatch(ctx context.Context, options watchOptions) error {
	if options.interval <= 0 || options.debounce <= 0 || options.stopGrace <= 0 {
		return errors.New("watch timing must be positive")
	}
	stamp, err := fileStamp(options.path)
	if err != nil {
		return err
	}
	child, err := startWatchedChild(options)
	if err != nil {
		return err
	}
	defer func() {
		if child != nil {
			_ = child.stop(options.stopGrace, nil)
		}
	}()
	fmt.Fprintf(options.stderr, "%s watching %s\n", ui.Dim("→"), options.path)
	ticker := time.NewTicker(options.interval)
	defer ticker.Stop()
	var candidate watchedStamp
	var candidateSince time.Time
	var candidateValid bool
	var restartPending bool
	var lastFileError string
	for {
		var childDone <-chan error
		if child != nil {
			childDone = child.done
		}
		select {
		case <-ctx.Done():
			if child != nil {
				_ = child.stop(options.stopGrace, nil)
				child = nil
			}
			return ctx.Err()
		case interrupted := <-options.interrupts:
			if child != nil {
				_ = child.stop(options.stopGrace, interrupted)
				child = nil
			}
			return &watchInterruptedError{signal: interrupted}
		case childErr := <-childDone:
			exitSignal := watchedProcessExitSignal(child.platform, childErr)
			child.releasePlatform()
			child = nil
			if exitSignal != nil {
				return &watchInterruptedError{signal: exitSignal}
			}
			if childErr != nil {
				fmt.Fprintf(options.stderr, "%s %v\n", ui.Red("wago:"), childErr)
			}
		case now := <-ticker.C:
			next, stampErr := fileStamp(options.path)
			if stampErr != nil {
				candidateValid = false
				message := stampErr.Error()
				if message != lastFileError {
					fmt.Fprintf(options.stderr, "%s watch: %v\n", ui.Red("wago:"), stampErr)
					lastFileError = message
				}
				continue
			}
			lastFileError = ""
			if next == stamp && !restartPending {
				candidateValid = false
				continue
			}
			if !candidateValid || next != candidate {
				candidate, candidateSince, candidateValid = next, now, true
				continue
			}
			if now.Sub(candidateSince) < options.debounce {
				continue
			}
			if child != nil {
				if err := child.stop(options.stopGrace, nil); err != nil {
					return err
				}
				child = nil
			}
			restartPending = true
			latest, stampErr := fileStamp(options.path)
			if stampErr != nil {
				candidateValid = false
				continue
			}
			if latest != candidate {
				candidate, candidateSince, candidateValid = latest, time.Now(), true
				continue
			}
			fmt.Fprintf(options.stderr, "%s changed %s\n", ui.Dim("→"), options.path)
			child, err = startWatchedChild(options)
			if err != nil {
				return err
			}
			stamp, candidateValid, restartPending = latest, false, false
		}
	}
}

type watchedStamp struct {
	size int64
	sum  [sha256.Size]byte
}

type watchHashBuffer [32 << 10]byte

var watchHashBuffers = sync.Pool{New: func() any { return new(watchHashBuffer) }}

func fileStamp(path string) (watchedStamp, error) {
	file, err := os.Open(path)
	if err != nil {
		return watchedStamp{}, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return watchedStamp{}, err
	}
	if !before.Mode().IsRegular() {
		return watchedStamp{}, fmt.Errorf("%s is not a regular file", path)
	}
	hash := sha256.New()
	buffer := watchHashBuffers.Get().(*watchHashBuffer)
	defer watchHashBuffers.Put(buffer)
	for {
		read, readErr := file.Read(buffer[:])
		if read != 0 {
			_, _ = hash.Write(buffer[:read])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return watchedStamp{}, readErr
		}
	}
	after, err := file.Stat()
	if err != nil {
		return watchedStamp{}, err
	}
	current, err := os.Stat(path)
	if err != nil {
		return watchedStamp{}, err
	}
	if before.Size() != after.Size() || before.ModTime() != after.ModTime() || !os.SameFile(after, current) {
		return watchedStamp{}, errors.New("module changed while it was read")
	}
	stamp := watchedStamp{size: after.Size()}
	hash.Sum(stamp.sum[:0])
	return stamp, nil
}

type watchedChild struct {
	command  *exec.Cmd
	platform watchedChildPlatform
	done     chan error
	release  sync.Once
}

func startWatchedChild(options watchOptions) (*watchedChild, error) {
	command := exec.Command(options.executable, options.arguments...)
	command.Stdin, command.Stdout, command.Stderr = options.stdin, options.stdout, options.stderr
	if options.environment != nil {
		command.Env = options.environment
	}
	prepareWatchedCommand(command)
	if err := command.Start(); err != nil {
		abortWatchedCommand(command)
		return nil, err
	}
	platform, err := attachWatchedProcess(command)
	if err != nil {
		abortWatchedCommand(command)
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, err
	}
	child := &watchedChild{command: command, platform: platform, done: make(chan error, 1)}
	go func() {
		err := command.Wait()
		child.done <- err
		close(child.done)
	}()
	return child, nil
}

func (child *watchedChild) stop(grace time.Duration, interrupt os.Signal) error {
	select {
	case <-child.done:
		child.releasePlatform()
		return nil
	default:
	}
	_ = interruptWatchedProcess(child.platform, child.command, interrupt)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-child.done:
		child.releasePlatform()
		return nil
	case <-timer.C:
	}
	if err := killWatchedProcess(child.platform, child.command); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-child.done
	child.releasePlatform()
	return nil
}

func (child *watchedChild) releasePlatform() {
	child.release.Do(func() { releaseWatchedProcess(child.platform, child.command) })
}

func withoutWatchFlags(arguments []string) []string {
	result := make([]string, 0, len(arguments))
	passThrough := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if passThrough {
			result = append(result, argument)
			continue
		}
		if argument == "--" {
			passThrough = true
			result = append(result, argument)
			continue
		}
		if argument == "--watch" || argument == "-w" {
			continue
		}
		if argument == "--watch-interval" {
			if index+1 < len(arguments) {
				index++
			}
			continue
		}
		if len(argument) > len("--watch-interval=") && argument[:len("--watch-interval=")] == "--watch-interval=" {
			continue
		}
		result = append(result, argument)
		if handoff.LooksLikeRuntimeTarget(argument) {
			passThrough = true
		}
	}
	return result
}
