//go:build !tinygo

package run

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestWithoutWatchFlagsPreservesGuestArguments(t *testing.T) {
	input := []string{"run", "--watch", "--watch-interval", "1s", "module.wasm", "--watch", "guest"}
	want := []string{"run", "module.wasm", "--watch", "guest"}
	if got := withoutWatchFlags(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("withoutWatchFlags = %#v, want %#v", got, want)
	}
}

func TestFileStampDetectsSameSizeRewriteWithRestoredModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "module.wasm")
	modTime := time.Unix(1_700_000_000, 0)
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	first, err := fileStamp(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("final"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	final, err := fileStamp(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == final {
		t.Fatal("same-size rewrite with restored timestamp was not detected")
	}
}

func TestWatchSupervisorRestartsLongRunningChildWithNewestContent(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.wasm")
	logPath := filepath.Join(dir, "starts.log")
	if err := os.WriteFile(modulePath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := false
	options := watchTestOptions(modulePath, logPath, address, false)
	options.debounce = 250 * time.Millisecond
	go func() {
		done <- superviseWatch(ctx, options)
	}()
	t.Cleanup(func() {
		cancel()
		if stopped {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watch supervisor did not stop")
		}
	})
	waitForWatchLog(t, logPath, 1)
	modTime := time.Unix(1_700_000_000, 0)
	if err := os.WriteFile(modulePath, []byte("middl"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(modulePath, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modulePath, []byte("final"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(modulePath, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	lines := waitForWatchLog(t, logPath, 2)
	if got, want := lines, []string{"first", "final"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("child starts = %#v, want %#v", got, want)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("supervisor exit = %v, want context cancellation", err)
	}
	stopped = true
}

func TestWatchSupervisorRestartsAfterChildExit(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.wasm")
	logPath := filepath.Join(dir, "starts.log")
	if err := os.WriteFile(modulePath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := false
	go func() {
		done <- superviseWatch(ctx, watchTestOptions(modulePath, logPath, "", true))
	}()
	t.Cleanup(func() {
		cancel()
		if stopped {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watch supervisor did not stop")
		}
	})
	waitForWatchLog(t, logPath, 1)
	if err := os.WriteFile(modulePath, []byte("final"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := waitForWatchLog(t, logPath, 2)
	if got, want := lines, []string{"first", "final"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("child starts = %#v, want %#v", got, want)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("supervisor exit = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch supervisor did not stop")
	}
	stopped = true
}

func TestWatchSupervisorHandlesDeleteAndRecreate(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.wasm")
	logPath := filepath.Join(dir, "starts.log")
	if err := os.WriteFile(modulePath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := false
	go func() {
		done <- superviseWatch(ctx, watchTestOptions(modulePath, logPath, "", false))
	}()
	t.Cleanup(func() {
		cancel()
		if stopped {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watch supervisor did not stop")
		}
	})
	waitForWatchLog(t, logPath, 1)
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(modulePath, []byte("final"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := waitForWatchLog(t, logPath, 2), []string{"first", "final"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("child starts = %#v, want %#v", got, want)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("supervisor exit = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch supervisor did not stop")
	}
	stopped = true
}

func TestWatchSupervisorForwardsInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows forwards console break through its process group")
	}
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.wasm")
	logPath := filepath.Join(dir, "starts.log")
	if err := os.WriteFile(modulePath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	interrupts := make(chan os.Signal, 1)
	options := watchTestOptions(modulePath, logPath, "", false)
	options.environment = append(options.environment, "WAGO_WATCH_SIGNAL=1")
	options.interrupts = interrupts
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := false
	go func() {
		done <- superviseWatch(ctx, options)
	}()
	t.Cleanup(func() {
		cancel()
		if stopped {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watch supervisor did not stop")
		}
	})
	waitForWatchLog(t, logPath, 1)
	interrupts <- os.Interrupt
	select {
	case err := <-done:
		var interrupted *watchInterruptedError
		if !errors.As(err, &interrupted) || interrupted.signal != os.Interrupt {
			t.Fatalf("supervisor exit = %v, want interrupt", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch supervisor did not forward interrupt")
	}
	stopped = true
	if got, want := waitForWatchLog(t, logPath, 2), []string{"first", "interrupt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("helper signal log = %#v, want %#v", got, want)
	}
}

func TestWatchSupervisorProxiesGuestInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows children keep direct console input")
	}
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.wasm")
	logPath := filepath.Join(dir, "starts.log")
	if err := os.WriteFile(modulePath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	options := watchTestOptions(modulePath, logPath, "", false)
	options.environment = append(options.environment, "WAGO_WATCH_STDIN=1")
	options.stdin = strings.NewReader("terminal-input")
	done := make(chan error, 1)
	stopped := false
	go func() { done <- superviseWatch(ctx, options) }()
	t.Cleanup(func() {
		cancel()
		if stopped {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watch supervisor did not stop")
		}
	})
	if got, want := waitForWatchLog(t, logPath, 1), []string{"terminal-input"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("helper input log = %#v, want %#v", got, want)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("supervisor exit = %v, want context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watch supervisor did not stop")
	}
	stopped = true
}

func watchTestOptions(modulePath, logPath, address string, exit bool) watchOptions {
	environment := append([]string(nil), os.Environ()...)
	environment = append(environment,
		"WAGO_WATCH_HELPER=1",
		"WAGO_WATCH_MODULE="+modulePath,
		"WAGO_WATCH_LOG="+logPath,
		"WAGO_WATCH_ADDRESS="+address,
	)
	if exit {
		environment = append(environment, "WAGO_WATCH_EXIT=1")
	}
	if address != "" {
		environment = append(environment, "WAGO_WATCH_TREE=1")
	}
	return watchOptions{
		path:        modulePath,
		interval:    15 * time.Millisecond,
		debounce:    60 * time.Millisecond,
		stopGrace:   250 * time.Millisecond,
		executable:  os.Args[0],
		arguments:   []string{"-test.run=^TestWatchHelperProcess$", "-test.count=1"},
		environment: environment,
		stdin:       nil,
		stdout:      io.Discard,
		stderr:      io.Discard,
	}
}

func waitForWatchLog(t *testing.T, path string, count int) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Fields(string(data))
			if len(lines) >= count {
				return lines
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("watch log did not reach %d entries", count)
	return nil
}

func TestWatchHelperProcess(t *testing.T) {
	if os.Getenv("WAGO_WATCH_HELPER") != "1" {
		return
	}
	if os.Getenv("WAGO_WATCH_TREE") == "1" && os.Getenv("WAGO_WATCH_LEAF") != "1" {
		// Give the watcher time to attach the new process to its platform process
		// group before this child creates a descendant.
		time.Sleep(100 * time.Millisecond)
		command := exec.Command(os.Args[0], "-test.run=^TestWatchHelperProcess$", "-test.count=1")
		command.Env = append(os.Environ(), "WAGO_WATCH_LEAF=1")
		command.Stdout, command.Stderr = io.Discard, io.Discard
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		select {}
	}
	if os.Getenv("WAGO_WATCH_STDIN") == "1" {
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			t.Fatalf("read watched stdin: %v", err)
		}
		appendWatchLog(t, string(input))
		return
	}
	var listener net.Listener
	if address := os.Getenv("WAGO_WATCH_ADDRESS"); address != "" {
		var err error
		listener, err = net.Listen("tcp", address)
		if err != nil {
			appendWatchLog(t, "overlap")
			return
		}
		defer listener.Close()
	}
	data, err := os.ReadFile(os.Getenv("WAGO_WATCH_MODULE"))
	if err != nil {
		t.Fatal(err)
	}
	var interrupts chan os.Signal
	if os.Getenv("WAGO_WATCH_SIGNAL") == "1" {
		interrupts = make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		defer signal.Stop(interrupts)
	}
	appendWatchLog(t, string(data))
	if os.Getenv("WAGO_WATCH_EXIT") == "1" {
		return
	}
	if interrupts != nil {
		<-interrupts
		appendWatchLog(t, "interrupt")
		return
	}
	select {}
}

func appendWatchLog(t *testing.T, value string) {
	t.Helper()
	file, err := os.OpenFile(os.Getenv("WAGO_WATCH_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkFileStamp64KiB(b *testing.B) {
	path := filepath.Join(b.TempDir(), "module.wasm")
	if err := os.WriteFile(path, make([]byte, 64<<10), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := fileStamp(path); err != nil {
			b.Fatal(err)
		}
	}
}
