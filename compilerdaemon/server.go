package compilerdaemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"

	"github.com/wago-org/wago"
)

// Limits bounds every allocation controlled directly by the daemon protocol.
type Limits struct {
	MaxOptionsBytes  uint64
	MaxWasmBytes     uint64
	MaxArtifactBytes uint64
	MaxErrorBytes    uint64
}

func DefaultLimits() Limits {
	return Limits{
		MaxOptionsBytes: 1 << 20, MaxWasmBytes: 512 << 20,
		MaxArtifactBytes: 1 << 30, MaxErrorBytes: 64 << 10,
	}
}

// Server owns one process-shared function cache and a bounded compilation
// semaphore. Construct it with NewServer before calling Serve or ServeConn.
type Server struct {
	Limits        Limits
	Cache         *wago.FunctionArtifactCache
	MaxConcurrent int

	once sync.Once
	sem  chan struct{}
}

// NewServer returns a daemon with a 256 MiB function cache and at most
// GOMAXPROCS simultaneous module compilations.
func NewServer() *Server {
	return &Server{
		Limits: DefaultLimits(), Cache: wago.NewFunctionArtifactCache(256 << 20),
		MaxConcurrent: max(runtime.GOMAXPROCS(0), 1),
	}
}

func (server *Server) initialize() {
	server.once.Do(func() {
		if server.Limits == (Limits{}) {
			server.Limits = DefaultLimits()
		}
		if server.Cache == nil {
			server.Cache = wago.NewFunctionArtifactCache(256 << 20)
		}
		if server.MaxConcurrent <= 0 {
			server.MaxConcurrent = max(runtime.GOMAXPROCS(0), 1)
		}
		server.sem = make(chan struct{}, server.MaxConcurrent)
	})
}

// Serve accepts reusable client connections until the listener closes or the
// context is cancelled. Each connection is sequential; different processes
// can compile concurrently within MaxConcurrent.
func (server *Server) Serve(ctx context.Context, listener net.Listener) error {
	if server == nil || listener == nil {
		return fmt.Errorf("compiler daemon: server and listener are required")
	}
	server.initialize()
	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-closed:
		}
	}()
	defer close(closed)
	var connections sync.WaitGroup
	defer connections.Wait()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("compiler daemon: accept: %w", err)
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			defer connection.Close()
			stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
			defer stop()
			_ = server.serveConnection(ctx, connection)
		}()
	}
}

// ServeConn serves a single reusable transport. It is primarily useful for an
// already-authenticated supervisor pipe and deterministic integration tests.
func (server *Server) ServeConn(ctx context.Context, connection net.Conn) error {
	if server == nil || connection == nil {
		return fmt.Errorf("compiler daemon: server and connection are required")
	}
	server.initialize()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	return server.serveConnection(ctx, connection)
}

func (server *Server) serveConnection(ctx context.Context, connection net.Conn) error {
	for {
		var encodedHeader [requestHeaderSize]byte
		if _, err := io.ReadFull(connection, encodedHeader[:]); err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("compiler daemon: read request: %w", err)
		}
		header, err := decodeRequestHeader(encodedHeader[:])
		if err != nil {
			return err
		}
		if uint64(header.OptionsLength) > server.Limits.MaxOptionsBytes || header.WasmLength > server.Limits.MaxWasmBytes || header.WasmLength > uint64(maxInt()) {
			if err := server.writeError(connection, header.ID, fmt.Errorf("compiler daemon: request exceeds configured limits")); err != nil {
				return err
			}
			return nil
		}
		encodedOptions := make([]byte, int(header.OptionsLength))
		wasm := make([]byte, int(header.WasmLength))
		if _, err := io.ReadFull(connection, encodedOptions); err != nil {
			return fmt.Errorf("compiler daemon: read options: %w", err)
		}
		if _, err := io.ReadFull(connection, wasm); err != nil {
			return fmt.Errorf("compiler daemon: read Wasm: %w", err)
		}
		if !requestHashMatches(wasm, header.WasmHash) {
			if err := server.writeError(connection, header.ID, fmt.Errorf("compiler daemon: Wasm digest mismatch")); err != nil {
				return err
			}
			continue
		}
		options, err := decodeCompileOptions(encodedOptions)
		if err == nil {
			select {
			case server.sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			var artifact []byte
			artifact, err = server.compile(options, wasm)
			<-server.sem
			if err == nil && uint64(len(artifact)) > server.Limits.MaxArtifactBytes {
				err = fmt.Errorf("compiler daemon: artifact exceeds configured limit")
			}
			if err == nil {
				if err := writeResponse(connection, responseHeader{ID: header.ID, Status: responseOK, Length: uint64(len(artifact)), Payload: sha256.Sum256(artifact)}, artifact); err != nil {
					return err
				}
				continue
			}
		}
		if err := server.writeError(connection, header.ID, err); err != nil {
			return err
		}
	}
}

func decodeCompileOptions(encoded []byte) (CompileOptions, error) {
	var options CompileOptions
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&options); err != nil {
		return CompileOptions{}, fmt.Errorf("compiler daemon: decode options: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			return CompileOptions{}, fmt.Errorf("compiler daemon: trailing options value")
		}
		return CompileOptions{}, fmt.Errorf("compiler daemon: trailing options data: %w", err)
	}
	return options, nil
}

func (server *Server) compile(options CompileOptions, wasm []byte) (artifact []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			artifact = nil
			err = fmt.Errorf("compiler daemon: compiler panic: %v", recovered)
		}
	}()
	config, err := options.runtimeConfig(server.Cache)
	if err != nil {
		return nil, err
	}
	compiled, err := config.Compile(wasm)
	if err != nil {
		return nil, err
	}
	defer compiled.Close()
	artifact, err = compiled.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("compiler daemon: serialize artifact: %w", err)
	}
	return artifact, nil
}

func (server *Server) writeError(connection io.Writer, id uint64, compileErr error) error {
	if compileErr == nil {
		compileErr = fmt.Errorf("compiler daemon: unknown compilation error")
	}
	payload := []byte(compileErr.Error())
	if uint64(len(payload)) > server.Limits.MaxErrorBytes {
		payload = payload[:server.Limits.MaxErrorBytes]
	}
	return writeResponse(connection, responseHeader{ID: id, Status: responseError, Length: uint64(len(payload)), Payload: sha256.Sum256(payload)}, payload)
}

func writeResponse(writer io.Writer, header responseHeader, payload []byte) error {
	var encoded [responseHeaderSize]byte
	encodeResponseHeader(encoded[:], header)
	if err := writeFull(writer, encoded[:]); err != nil {
		return fmt.Errorf("compiler daemon: write response header: %w", err)
	}
	if err := writeFull(writer, payload); err != nil {
		return fmt.Errorf("compiler daemon: write response payload: %w", err)
	}
	return nil
}

func maxInt() int { return int(^uint(0) >> 1) }
