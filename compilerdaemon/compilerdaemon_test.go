package compilerdaemon

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/wago-org/wago"
)

var daemonIdentityModule = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x07, 0x01, 0x03, 'r', 'u', 'n', 0x00, 0x00,
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x20, 0x00, 0x0b,
}

func TestClientServerCompileValidatesArtifactAndReusesFunctionCache(t *testing.T) {
	serverConnection, clientConnection := net.Pipe()
	server := NewServer()
	server.Cache = wago.NewFunctionArtifactCache(1 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ServeConn(ctx, serverConnection)
	}()
	client, err := NewClient(clientConnection)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = client.Close()
		cancel()
		if err := <-serverErr; err != nil {
			t.Errorf("server close: %v", err)
		}
	}()

	options := CompileOptions{Target: wago.TargetCompatibility, Objective: wago.OptimizeSpeed, Core: 1}
	compiled, err := client.Compile(ctx, options, append([]byte(nil), daemonIdentityModule...))
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Compiler() != wago.CompilerDragline {
		t.Fatalf("compiler = %s, want Dragline", compiled.Compiler())
	}
	instance, err := wago.Instantiate(compiled, wago.InstantiateOptions{})
	if err != nil {
		compiled.Close()
		t.Fatal(err)
	}
	result, err := instance.Invoke("run", wago.I32(41))
	if err != nil || len(result) != 1 || wago.AsI32(result[0]) != 41 {
		t.Fatalf("run(41) = %v, %v", result, err)
	}
	instance.Close()
	compiled.Close()

	compiled, err = client.Compile(ctx, options, append([]byte(nil), daemonIdentityModule...))
	if err != nil {
		t.Fatal(err)
	}
	compiled.Close()
	stats := server.Cache.Stats()
	if stats.Entries == 0 || stats.Hits == 0 {
		t.Fatalf("shared function cache did not warm: %#v", stats)
	}
}

func TestRemoteCompileErrorKeepsConnectionReusable(t *testing.T) {
	serverConnection, clientConnection := net.Pipe()
	server := NewServer()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.ServeConn(ctx, serverConnection) }()
	client, err := NewClient(clientConnection)
	if err != nil {
		t.Fatal(err)
	}
	options := CompileOptions{Target: wago.TargetCompatibility, Objective: wago.OptimizeSpeed, Core: 1}
	if _, err := client.Compile(ctx, options, []byte{0}); err == nil {
		t.Fatal("invalid Wasm compiled")
	} else {
		var remote *RemoteError
		if !errors.As(err, &remote) {
			t.Fatalf("invalid Wasm error = %T %v, want RemoteError", err, err)
		}
	}
	compiled, err := client.Compile(ctx, options, append([]byte(nil), daemonIdentityModule...))
	if err != nil {
		t.Fatalf("connection did not survive remote error: %v", err)
	}
	compiled.Close()
	_ = client.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeSharesCacheAcrossClientConnections(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer()
	server.Cache = wago.NewFunctionArtifactCache(1 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	options := CompileOptions{Target: wago.TargetCompatibility, Objective: wago.OptimizeSpeed, Core: 1}
	for connection := 0; connection < 2; connection++ {
		client, err := Dial(ctx, "tcp4", listener.Addr().String())
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		compiled, err := client.Compile(ctx, options, append([]byte(nil), daemonIdentityModule...))
		if err != nil {
			_ = client.Close()
			cancel()
			t.Fatal(err)
		}
		compiled.Close()
		if err := client.Close(); err != nil {
			cancel()
			t.Fatal(err)
		}
	}
	stats := server.Cache.Stats()
	if stats.Hits == 0 {
		cancel()
		t.Fatalf("second client did not share warm cache: %#v", stats)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsUnvalidatedArtifact(t *testing.T) {
	serverConnection, clientConnection := net.Pipe()
	done := make(chan error, 1)
	go func() {
		defer serverConnection.Close()
		var request [requestHeaderSize]byte
		if _, err := io.ReadFull(serverConnection, request[:]); err != nil {
			done <- err
			return
		}
		header, err := decodeRequestHeader(request[:])
		if err != nil {
			done <- err
			return
		}
		payload := make([]byte, int(uint64(header.OptionsLength)+header.WasmLength))
		if _, err := io.ReadFull(serverConnection, payload); err != nil {
			done <- err
			return
		}
		invalid := []byte("not a compiled Wago artifact")
		err = writeResponse(serverConnection, responseHeader{ID: header.ID, Status: responseOK, Length: uint64(len(invalid)), Payload: sha256.Sum256(invalid)}, invalid)
		done <- err
	}()
	client, err := NewClient(clientConnection)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Compile(context.Background(), CompileOptions{Target: wago.TargetCompatibility, Objective: wago.OptimizeSpeed, Core: 1}, daemonIdentityModule)
	if err == nil {
		t.Fatal("unvalidated daemon artifact was accepted")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
