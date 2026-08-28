// Command draglined runs the optional out-of-process Dragline compiler.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/compilerdaemon"
	"github.com/wago-org/wago/internal/atomicfile"
)

func main() {
	if err := run(); err != nil {
		fatalf("%v", err)
	}
}

func run() error {
	network, address := defaultEndpoint()
	flag.StringVar(&network, "network", network, "listener network: unix, tcp, tcp4, or tcp6")
	flag.StringVar(&address, "address", address, "listener address")
	cacheBytes := flag.Uint64("cache-bytes", 256<<20, "hard byte bound for the shared function cache")
	cacheFile := flag.String("cache-file", "", "optional persistent function-cache snapshot")
	workers := flag.Int("workers", runtime.GOMAXPROCS(0), "maximum concurrent module compilations")
	maxWasm := flag.Uint64("max-wasm-bytes", 512<<20, "maximum source module bytes per request")
	maxArtifact := flag.Uint64("max-artifact-bytes", 1<<30, "maximum compiled artifact bytes per response")
	allowTCP := flag.Bool("allow-unauthenticated-loopback", false, "allow an unauthenticated loopback TCP listener")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flag.Arg(0))
	}
	if *workers <= 0 || *cacheBytes == 0 || *maxWasm == 0 || *maxArtifact == 0 {
		return fmt.Errorf("workers and byte limits must be positive")
	}
	server := compilerdaemon.NewServer()
	server.Cache = wago.NewFunctionArtifactCache(*cacheBytes)
	if err := loadCacheSnapshot(server.Cache, *cacheFile); err != nil {
		return fmt.Errorf("load cache: %w", err)
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	if err := secureListener(network, listener, *allowTCP); err != nil {
		return fmt.Errorf("listener: %w", err)
	}
	server.MaxConcurrent = *workers
	server.Limits.MaxWasmBytes = *maxWasm
	server.Limits.MaxArtifactBytes = *maxArtifact
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintln(os.Stdout, listener.Addr().String())
	serveErr := server.Serve(ctx, listener)
	saveErr := saveCacheSnapshot(server.Cache, *cacheFile)
	if serveErr != nil {
		return fmt.Errorf("serve: %w", serveErr)
	}
	if saveErr != nil {
		return fmt.Errorf("save cache: %w", saveErr)
	}
	return nil
}

func loadCacheSnapshot(cache *wago.FunctionArtifactCache, path string) error {
	if path == "" {
		return nil
	}
	file, err := openRegularFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := cache.RestoreFrom(file); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func saveCacheSnapshot(cache *wago.FunctionArtifactCache, path string) error {
	if path == "" {
		return nil
	}
	return atomicfile.ReplaceFile(path, atomicfile.Options{Mode: 0o600, Sync: true}, func(writer io.Writer) error {
		_, err := cache.SnapshotTo(writer)
		return err
	})
}

func openRegularFile(path string) (*os.File, error) {
	linked, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if linked.Mode()&os.ModeSymlink != 0 || !linked.Mode().IsRegular() {
		return nil, fmt.Errorf("cache snapshot %s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(opened, linked) {
		_ = file.Close()
		return nil, fmt.Errorf("cache snapshot %s changed while opening", path)
	}
	return file, nil
}

func defaultEndpoint() (network, address string) {
	if runtime.GOOS == "windows" {
		return "tcp4", "127.0.0.1:0"
	}
	return "unix", filepath.Join(os.TempDir(), "wago-draglined.sock")
}

func secureListener(network string, listener net.Listener, allowTCP bool) error {
	switch network {
	case "unix", "unixpacket":
		return os.Chmod(listener.Addr().String(), 0o600)
	case "tcp", "tcp4", "tcp6":
		address, ok := listener.Addr().(*net.TCPAddr)
		if !ok || !address.IP.IsLoopback() {
			return fmt.Errorf("TCP listener must resolve to a loopback address")
		}
		if !allowTCP {
			return fmt.Errorf("TCP requires -allow-unauthenticated-loopback; prefer a mode-0600 Unix socket")
		}
		return nil
	default:
		return fmt.Errorf("unsupported network %q", network)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "draglined: "+format+"\n", args...)
	os.Exit(1)
}
