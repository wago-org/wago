package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/wago-org/wago"
	"github.com/wago-org/wago/compilerdaemon"
)

var cacheIdentityModule = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x07, 0x01, 0x03, 'r', 'u', 'n', 0x00, 0x00,
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x20, 0x00, 0x0b,
}

func TestDefaultEndpointIsLocal(t *testing.T) {
	network, address := defaultEndpoint()
	if network == "" || address == "" {
		t.Fatalf("default endpoint = %q %q", network, address)
	}
	if runtime.GOOS == "windows" && network != "tcp4" || runtime.GOOS != "windows" && network != "unix" {
		t.Fatalf("default network = %q on %s", network, runtime.GOOS)
	}
}

func TestSecureListenerRequiresExplicitLoopbackTCP(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := secureListener("tcp4", listener, false); err == nil {
		t.Fatal("unauthenticated loopback listener was enabled implicitly")
	}
	if err := secureListener("tcp4", listener, true); err != nil {
		t.Fatal(err)
	}
}

func TestSecureListenerRejectsNonLoopbackTCP(t *testing.T) {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := secureListener("tcp4", listener, true); err == nil {
		t.Fatal("non-loopback compiler listener was accepted")
	}
}

func TestSecureUnixListenerIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets are unavailable")
	}
	path := fmt.Sprintf("/tmp/wago-dmn-%d-%d.sock", os.Getpid(), time.Now().UnixNano())
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := secureListener("unix", listener, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket permissions = %o, want 600", got)
	}
}

func TestPersistentCacheSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "functions.cache")
	first := wago.NewFunctionArtifactCache(1 << 20)
	compileThroughServer(t, first)
	if stats := first.Stats(); stats.Entries == 0 {
		t.Fatalf("compiled cache is empty: %#v", stats)
	}
	if err := saveCacheSnapshot(first, path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cache permissions = %o, want 600", got)
	}

	restored := wago.NewFunctionArtifactCache(1 << 20)
	if err := loadCacheSnapshot(restored, path); err != nil {
		t.Fatal(err)
	}
	compileThroughServer(t, restored)
	if stats := restored.Stats(); stats.Entries == 0 || stats.Hits == 0 {
		t.Fatalf("restored cache did not produce a hit: %#v", stats)
	}
}

func TestPersistentCacheRejectsMalformedAndSymlinkSnapshots(t *testing.T) {
	directory := t.TempDir()
	malformed := filepath.Join(directory, "malformed.cache")
	if err := os.WriteFile(malformed, []byte("not a cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := wago.NewFunctionArtifactCache(1 << 20)
	if err := loadCacheSnapshot(cache, malformed); err == nil {
		t.Fatal("malformed cache restored")
	}
	if stats := cache.Stats(); stats.Entries != 0 {
		t.Fatalf("failed restore mutated cache: %#v", stats)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(directory, "link.cache")
		if err := os.Symlink(malformed, link); err != nil {
			t.Fatal(err)
		}
		if err := loadCacheSnapshot(cache, link); err == nil {
			t.Fatal("symlink cache restored")
		}
		if err := saveCacheSnapshot(cache, link); err == nil {
			t.Fatal("symlink cache overwritten")
		}
	}
}

func compileThroughServer(t *testing.T, cache *wago.FunctionArtifactCache) {
	t.Helper()
	serverConnection, clientConnection := net.Pipe()
	server := compilerdaemon.NewServer()
	server.Cache = cache
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ServeConn(ctx, serverConnection) }()
	client, err := compilerdaemon.NewClient(clientConnection)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	compiled, err := client.Compile(ctx, compilerdaemon.CompileOptions{
		Target: wago.TargetCompatibility, Objective: wago.OptimizeSpeed, Core: 1,
	}, cacheIdentityModule)
	if err != nil {
		_ = client.Close()
		cancel()
		t.Fatal(err)
	}
	compiled.Close()
	_ = client.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
