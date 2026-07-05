package wago

import (
	"errors"
	"fmt"
)

// Version is the wago runtime version, compared against an extension's MinWago at
// Use time so a runtime rejects an extension that needs a newer host.
const Version = "0.1.0"

// Stability marks how settled an extension's public surface is.
type Stability string

const (
	Experimental Stability = "experimental"
	Stable       Stability = "stable"
	Deprecated   Stability = "deprecated"
)

// ExtensionInfo is an extension's self-description: a stable ID, human metadata,
// the minimum wago version it needs, and its stability level. IDs should be
// dotted and stable (e.g. "wago.timer", "company.redis").
type ExtensionInfo struct {
	ID          string
	Name        string
	Version     string
	Description string
	MinWago     string
	Stability   Stability
}

// Extension is the one interface an extension author implements. Everything an
// extension contributes — host imports, capabilities, hooks — is declared through
// the Registry inside Register; the runtime owns orchestration.
type Extension interface {
	Info() ExtensionInfo
	Register(reg *Registry) error
}

// Capability names a coarse permission an extension provides and a policy can
// allow or deny. Names are stable strings so they can appear in configs and
// audit output.
type Capability string

const (
	CapTimerRead       Capability = "timer.read"
	CapProcessSpawn    Capability = "process.spawn"
	CapProcessKill     Capability = "process.kill"
	CapMailboxSend     Capability = "mailbox.send"
	CapMailboxReceive  Capability = "mailbox.receive"
	CapNetworkOutbound Capability = "net.outbound"
	CapFilesystemRead  Capability = "fs.read"
	CapFilesystemWrite Capability = "fs.write"
	CapHTTPClient      Capability = "http.client"
	CapKVRead          Capability = "kv.read"
	CapKVWrite         Capability = "kv.write"
	CapMetricsWrite    Capability = "metrics.write"
	CapCompilerCodegen Capability = "compiler.codegen"
)

// Extension-layer sentinel errors. Wrap them with ExtensionError to attach the
// offending extension and operation.
var (
	ErrPermissionDenied  = errors.New("wago: permission denied")
	ErrMissingImport     = errors.New("wago: missing import")
	ErrInvalidHandle     = errors.New("wago: invalid handle")
	ErrExtensionConflict = errors.New("wago: extension conflict")
)

// ExtensionError attributes a failure to a specific extension and operation while
// preserving the underlying error for errors.Is/As.
type ExtensionError struct {
	Extension string
	Operation string
	Err       error
}

func (e *ExtensionError) Error() string {
	return fmt.Sprintf("wago extension %s: %s: %v", e.Extension, e.Operation, e.Err)
}

func (e *ExtensionError) Unwrap() error { return e.Err }
