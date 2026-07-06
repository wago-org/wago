// Package log provides a logging host import for wasm guests under the
// "wago_log" module.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"

	wago "github.com/wago-org/wago"
)

// Level is a log severity passed by the guest.
type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelDebug:
		return "DEBUG"
	default:
		return fmt.Sprintf("LVL%d", int32(l))
	}
}

// Extension is the logging extension.
type Extension struct {
	mu sync.Mutex
	w  io.Writer
}

// Option configures the log extension.
type Option func(*Extension)

// WithWriter sets the destination writer (default os.Stderr).
func WithWriter(w io.Writer) Option { return func(e *Extension) { e.w = w } }

// Ext constructs the log extension.
func Ext(opts ...Option) *Extension {
	e := &Extension{w: os.Stderr}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Info identifies the extension.
func (e *Extension) Info() wago.ExtensionInfo {
	return wago.ExtensionInfo{
		ID:          "wago.log",
		Name:        "Log",
		Version:     "1.0.0",
		Description: "Structured logging for wasm guests.",
		Stability:   wago.Stable,
		Homepage:    "https://github.com/wago-org/wago",
		Repository:  "https://github.com/wago-org/wago",
		License:     "Apache-2.0",
		Authors:     []string{"The wago authors"},
		Tags:        []string{"logging", "observability"},
		Compat: wago.Compatibility{
			Engines:   map[string]string{"wago": ">=0.1.0", "tinygo": "*"},
			Platforms: []string{"linux/amd64"},
		},
	}
}

// Register wires the wago_log host import.
func (e *Extension) Register(reg *wago.Registry) error {
	reg.ImportModule("wago_log").
		Func("write", func(m wago.HostModule, p, res []uint64) {
			level := Level(wago.AsI32(p[0]))
			ptr, n := uint32(p[1]), uint32(p[2])
			mem := m.Memory()
			if int64(ptr)+int64(n) > int64(len(mem)) {
				res[0] = wago.I32(4) // buffer out of range
				return
			}
			e.write(level, mem[ptr:ptr+n])
			res[0] = wago.I32(0)
		}).
		Params(wago.ValI32, wago.ValI32, wago.ValI32).Results(wago.ValI32).
		Docs("write a log message: (level i32, ptr i32, len i32) -> status i32")
	return nil
}

func (e *Extension) write(level Level, msg []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fmt.Fprintf(e.w, "[%s] %s\n", level, msg)
}
