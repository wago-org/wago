package compilerdaemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/wago-org/wago"
)

// RemoteError is a bounded error reported by the compiler process. It leaves a
// reusable connection synchronized for a later request.
type RemoteError struct{ Message string }

func (err *RemoteError) Error() string { return err.Message }

// Client serializes requests over one reusable connection. Returned artifacts
// are trusted as products of the configured compiler daemon, then pass Wago's
// artifact decoder and native metadata checks before being returned.
type Client struct {
	connection net.Conn
	limits     Limits

	mu     sync.Mutex
	nextID uint64
	broken bool
}

func NewClient(connection net.Conn) (*Client, error) {
	if connection == nil {
		return nil, fmt.Errorf("compiler daemon: client connection is required")
	}
	return &Client{connection: connection, limits: DefaultLimits()}, nil
}

// Dial connects to a draglined listener and returns a validating reusable
// client. Unix sockets are recommended; TCP listeners require an explicit
// server-side opt-in and should remain loopback-only.
func Dial(ctx context.Context, network, address string) (*Client, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("compiler daemon: dial: %w", err)
	}
	client, err := NewClient(connection)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	return client, nil
}

// SetLimits narrows client-side response/request limits. Call it before the
// first Compile; zero fields retain the default for that field.
func (client *Client) SetLimits(limits Limits) {
	client.mu.Lock()
	defer client.mu.Unlock()
	defaults := DefaultLimits()
	if limits.MaxOptionsBytes != 0 {
		defaults.MaxOptionsBytes = limits.MaxOptionsBytes
	}
	if limits.MaxWasmBytes != 0 {
		defaults.MaxWasmBytes = limits.MaxWasmBytes
	}
	if limits.MaxArtifactBytes != 0 {
		defaults.MaxArtifactBytes = limits.MaxArtifactBytes
	}
	if limits.MaxErrorBytes != 0 {
		defaults.MaxErrorBytes = limits.MaxErrorBytes
	}
	client.limits = defaults
}

func (client *Client) Compile(ctx context.Context, options CompileOptions, wasm []byte) (*wago.Compiled, error) {
	if client == nil {
		return nil, fmt.Errorf("compiler daemon: nil client")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.broken || client.connection == nil {
		return nil, fmt.Errorf("compiler daemon: client connection is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uint64(len(wasm)) > client.limits.MaxWasmBytes {
		return nil, fmt.Errorf("compiler daemon: Wasm exceeds client limit")
	}
	if _, err := options.runtimeConfig(nil); err != nil {
		return nil, err
	}
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("compiler daemon: encode options: %w", err)
	}
	if uint64(len(encodedOptions)) > client.limits.MaxOptionsBytes || uint64(len(encodedOptions)) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("compiler daemon: options exceed client limit")
	}
	client.nextID++
	if client.nextID == 0 {
		client.nextID++
	}
	id := client.nextID
	deadline := time.Time{}
	if configured, ok := ctx.Deadline(); ok {
		deadline = configured
	}
	if err := client.connection.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("compiler daemon: set deadline: %w", err)
	}
	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		_ = client.connection.SetDeadline(time.Now())
		close(cancelDone)
	})
	defer func() {
		if !stopCancel() {
			<-cancelDone
		}
		_ = client.connection.SetDeadline(time.Time{})
	}()
	var encodedHeader [requestHeaderSize]byte
	encodeRequestHeader(encodedHeader[:], requestHeader{
		ID: id, OptionsLength: uint32(len(encodedOptions)), WasmLength: uint64(len(wasm)), WasmHash: sha256.Sum256(wasm),
	})
	for _, data := range [][]byte{encodedHeader[:], encodedOptions, wasm} {
		if err := writeFull(client.connection, data); err != nil {
			client.breakConnection()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("compiler daemon: write request: %w", err)
		}
	}
	var encodedResponse [responseHeaderSize]byte
	if _, err := io.ReadFull(client.connection, encodedResponse[:]); err != nil {
		client.breakConnection()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("compiler daemon: read response header: %w", err)
	}
	header, err := decodeResponseHeader(encodedResponse[:])
	if err != nil || header.ID != id {
		client.breakConnection()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("compiler daemon: response id %d does not match request %d", header.ID, id)
	}
	limit := client.limits.MaxArtifactBytes
	if header.Status == responseError {
		limit = client.limits.MaxErrorBytes
	}
	if header.Length > limit || header.Length > uint64(maxInt()) {
		client.breakConnection()
		return nil, fmt.Errorf("compiler daemon: response exceeds client limit")
	}
	payload := make([]byte, int(header.Length))
	if _, err := io.ReadFull(client.connection, payload); err != nil {
		client.breakConnection()
		return nil, fmt.Errorf("compiler daemon: read response payload: %w", err)
	}
	if sha256.Sum256(payload) != header.Payload {
		client.breakConnection()
		return nil, fmt.Errorf("compiler daemon: response digest mismatch")
	}
	if header.Status == responseError {
		return nil, &RemoteError{Message: string(payload)}
	}
	compiled, err := wago.LoadTrustedArtifact(payload)
	if err != nil {
		client.breakConnection()
		return nil, fmt.Errorf("compiler daemon: validate artifact: %w", err)
	}
	if compiled.Compiler() != wago.CompilerDragline && options.Fallback != wago.CompilerFallbackRailshot {
		compiled.Close()
		client.breakConnection()
		return nil, fmt.Errorf("compiler daemon: returned artifact has compiler %s", compiled.Compiler())
	}
	return compiled, nil
}

func (client *Client) breakConnection() {
	client.broken = true
	_ = client.connection.Close()
}

func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.connection == nil {
		return nil
	}
	err := client.connection.Close()
	client.connection = nil
	client.broken = true
	return err
}
