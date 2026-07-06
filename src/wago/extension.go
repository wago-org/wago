package wago

import "fmt"

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

// Compatibility describes the environments an extension supports, so a runtime or
// a build tool can check fit before wiring it in. Empty fields mean "no
// constraint" — an extension that omits Compatibility entirely is treated as
// compatible with any wago version and platform.
type Compatibility struct {
	// MinWago / MaxWago bound the wago runtime version (semver, inclusive). The
	// runtime rejects an extension at Use time if the running Version falls outside.
	MinWago string `json:"minWago,omitempty"`
	MaxWago string `json:"maxWago,omitempty"`
	// TinyGo is true if the extension compiles and works under TinyGo (no cgo, no
	// reflection). The stack-form HostFunc keeps this achievable.
	TinyGo bool `json:"tinygo"`
	// Platforms lists supported GOOS/GOARCH pairs (e.g. "linux/amd64"). Empty means
	// the extension is platform-independent (pure Go host functions).
	Platforms []string `json:"platforms,omitempty"`
	// GoVersion is the minimum Go toolchain the extension needs (e.g. "1.22"),
	// informational for build tooling. Empty means no explicit floor.
	GoVersion string `json:"goVersion,omitempty"`
}

// ExtensionInfo is an extension's self-description: identity, human metadata,
// provenance, and the environments it supports. It is what `wago plugin inspect`
// and `wago plugin list` surface, and what the runtime checks for compatibility at
// Use time. IDs should be dotted and stable (e.g. "wago.timer", "company.redis").
type ExtensionInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Version     string    `json:"version,omitempty"` // extension version (semver)
	Description string    `json:"description,omitempty"`
	Stability   Stability `json:"stability,omitempty"`

	// Provenance.
	Homepage   string   `json:"homepage,omitempty"`   // project or docs URL
	Repository string   `json:"repository,omitempty"` // source repo, e.g. https://github.com/acme/wago-redis
	License    string   `json:"license,omitempty"`    // SPDX identifier, e.g. "Apache-2.0"
	Authors    []string `json:"authors,omitempty"`    // "Name <email>" entries
	Keywords   []string `json:"keywords,omitempty"`   // discovery tags

	// Compat records the wago versions, platforms, and TinyGo support this
	// extension is known to work with.
	Compat Compatibility `json:"compatibility"`
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
	CapWASI            Capability = "wasi"
)

// extErr is a comparable, constant error type so the extension-layer sentinels
// can be package-level consts (the root facade re-exports consts but not vars)
// while still working with errors.Is.
type extErr string

func (e extErr) Error() string { return string(e) }

// Extension-layer sentinel errors. Wrap them with ExtensionError to attach the
// offending extension and operation; match them with errors.Is.
const (
	ErrPermissionDenied  = extErr("wago: permission denied")
	ErrMissingImport     = extErr("wago: missing import")
	ErrInvalidHandle     = extErr("wago: invalid handle")
	ErrExtensionConflict = extErr("wago: extension conflict")
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
