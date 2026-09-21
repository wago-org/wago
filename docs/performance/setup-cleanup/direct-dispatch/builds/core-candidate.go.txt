// Package core implements the shared internals of the Preview 1 provider.
// It is internal: use github.com/wago-org/wasi/p1.
package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	wago "github.com/wago-org/wago"
)

// Guest capabilities are deliberately narrower than "WASI". A policy can
// allow stdout without filesystem mutation, argv without environment access,
// or clocks without randomness. Descriptor and path capabilities remain
// coarse enough to match Preview 1's descriptor-multiplexed ABI honestly.
const (
	CapFDRead          wago.Capability = "wasi.fd.read"
	CapFDWrite         wago.Capability = "wasi.fd.write"
	CapFDManage        wago.Capability = "wasi.fd.manage"
	CapPathRead        wago.Capability = "wasi.path.read"
	CapPathOpen        wago.Capability = "wasi.path.open"
	CapPathWrite       wago.Capability = "wasi.path.write"
	CapArgumentsRead   wago.Capability = "wasi.arguments.read"
	CapEnvironmentRead wago.Capability = "wasi.environment.read"
	CapClockRead       wago.Capability = "wasi.clock.read"
	CapRandomRead      wago.Capability = "wasi.random.read"
	CapProcessExit     wago.Capability = "wasi.process.exit"
	CapPoll            wago.Capability = "wasi.poll"
	CapSchedulerYield  wago.Capability = "wasi.scheduler.yield"
	CapUnsupported     wago.Capability = "wasi.unsupported"
)

// WASI errno values (subset used here); identical across snapshots.
const (
	wasiOK      = 0
	wasiEBadf   = 8
	wasiEInval  = 28
	wasiESpipe  = 70
	wasiENotsup = 58
)

// Config configures the WASI host bundle. A nil writer/reader discards/EOFs;
// nil Clocks uses separate system realtime and monotonic clocks; a nil Rand
// uses crypto/rand.
type Config struct {
	Stdout, Stderr io.Writer
	Stdin          io.Reader
	Args           []string        // argv; Args[0] is conventionally the program name
	Env            []string        // "KEY=VALUE" entries
	Clocks         ClockSource     // distinct realtime, monotonic, and optional CPU clocks
	Context        context.Context // cancellation for blocking host operations
	Rand           io.Reader       // random source for random_get
	// Mounts is the rights-aware preopen configuration. No rights are implied.
	Mounts []Preopen
	// MaxOpenFiles bounds the host descriptors owned by one guest instance,
	// including stdio and preopens. Zero uses the secure default of 1024.
	MaxOpenFiles uint32
	// MaxIOVecs and MaxSubscriptionsPerPoll bound guest-controlled host slices.
	// Zero uses 1024 for each limit.
	MaxIOVecs               uint32
	MaxSubscriptionsPerPoll uint32
}

// Preopen grants explicit filesystem rights beneath one host directory.
type Preopen struct {
	GuestPath       string `json:"guest"`
	HostPath        string `json:"host"`
	Read            bool   `json:"read"`
	Write           bool   `json:"write"`
	MutateDirectory bool   `json:"mutateDirectory"`
}

type pluginConfig struct {
	Stdin            *string    `json:"stdin,omitempty"`
	Stdout           *string    `json:"stdout,omitempty"`
	Stderr           *string    `json:"stderr,omitempty"`
	Env              *[]string  `json:"env,omitempty"`
	Mounts           *[]Preopen `json:"mounts,omitempty"`
	MaxOpenFiles     *uint32    `json:"maxOpenFiles,omitempty"`
	MaxIOVecs        *uint32    `json:"maxIOVecs,omitempty"`
	MaxSubscriptions *uint32    `json:"maxSubscriptionsPerPoll,omitempty"`
}

var configSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "stdin": {"type": "string", "enum": ["inherit", "eof"]},
    "stdout": {"type": "string", "enum": ["inherit", "discard"]},
    "stderr": {"type": "string", "enum": ["inherit", "discard"]},
    "env": {
      "type": "array",
      "maxItems": 4096,
      "items": {"type": "string", "minLength": 2, "maxLength": 32768, "pattern": "^[^=\\u0000]+=[^\\u0000]*$"}
    },
    "mounts": {
      "type": "array",
      "maxItems": 64,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["guest", "host"],
        "properties": {
          "guest": {"type": "string", "pattern": "^/(?:[^/\\u0000]+(?:/[^/\\u0000]+)*)?$", "maxLength": 4096},
          "host": {"type": "string", "minLength": 1, "maxLength": 4096},
          "read": {"type": "boolean"},
          "write": {"type": "boolean"},
          "mutateDirectory": {"type": "boolean"}
        }
      }
    },
    "maxOpenFiles": {"type": "integer", "minimum": 3, "maximum": 65536},
    "maxIOVecs": {"type": "integer", "minimum": 1, "maximum": 65536},
    "maxSubscriptionsPerPoll": {"type": "integer", "minimum": 1, "maximum": 65536}
  }
}`)

const maxConfigBytes = 256 << 10

// ConfigSchema returns a fresh copy of the strict JSON configuration schema
// embedded in every WASI provider definition.
func ConfigSchema() json.RawMessage {
	return append(json.RawMessage(nil), configSchema...)
}

// Provider builds one side-effect-free catalog entry. The caller supplies the
// immutable package-specific definition and exact Preview 1 import module.
func Provider(definition wago.PluginDefinition, module string) wago.PluginProvider {
	return wago.PluginProvider{
		Definition: definition,
		New: func() wago.Plugin {
			return &Plugin{module: module}
		},
		ValidateConfig: validatePluginConfig,
	}
}

// Plugin is a WASI provider bound to one exact Wasm import module. It has no
// package-global registration or process-global argv state.
type Plugin struct {
	module    string
	cfg       Config
	arguments *wago.GuestArgumentsAccess
	fs        *fsState
	guard     *fsGuard
}

// Register declares imports, guest capabilities, and lifecycle through exact
// vNext handles. Filesystem resources are opened only during Start.
func (e *Plugin) Register(reg *wago.Registrar) error {
	var cfg pluginConfig
	if err := reg.Config(&cfg); err != nil {
		return err
	}
	resolved, err := configFromPluginConfig(cfg)
	if err != nil {
		return err
	}
	e.cfg = resolved
	e.resetFS()

	e.arguments, err = reg.GuestArguments()
	if err != nil {
		return err
	}
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	e.guard.resolver, err = reg.HostCallers()
	if err != nil {
		return err
	}
	closed, err := reg.InstanceCloseObserver()
	if err != nil {
		return err
	}
	if err := closed.After(func(event wago.InstanceCloseEvent) { e.closeInstance(event.Instance) }); err != nil {
		return err
	}
	for _, capability := range guestCapabilities {
		if err := reg.GuestCapability(capability.cap, wago.CapabilityDocs(capability.docs)); err != nil {
			return err
		}
	}
	for _, b := range importBindings {
		imports.HostFunc(e.module, b.name, b.callback(e)).Params(b.params...).Results(b.results...).Capability(b.cap).Docs(b.docs)
	}
	return reg.Lifecycle(wago.PluginLifecycle{Start: e.start, Stop: e.stop})
}

func (e *Plugin) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	args, err := e.arguments.Args()
	if err != nil {
		return err
	}
	e.cfg.Args = args
	return e.initFS(true)
}

func (e *Plugin) stop(context.Context) error {
	e.closeAll()
	return nil
}

// Imports returns one stateful raw low-level host bundle for one instance. Call
// Imports again for each additional instance. This API intentionally remains
// available for embedders that do not use Runtime.LoadPlugins. Plugin policy,
// lifecycle cleanup, and runtime-scoped argv apply only to Provider.
func Imports(module string, cfg Config) *wago.Imports {
	e := &Plugin{module: module, cfg: cloneConfig(cfg)}
	e.resetFS()
	_ = e.initFS(false) // raw imports cannot report mount initialization errors
	return e.Imports()
}

func (e *Plugin) Imports() *wago.Imports {
	out := wago.NewImports()
	for _, b := range importBindings {
		out.HostFunc(e.module, b.name, b.callback(e)).Params(b.params...).Results(b.results...).Capability(b.cap).Docs(b.docs)
	}
	return out
}

// binding is one host function with its declared signature and docs. Register and
// Imports both use the private definition table, so their declarations agree.
type binding struct {
	name            string
	handler         handlerID
	params, results []wago.ValType
	cap             wago.Capability
	docs            string
}

func (b binding) callback(e *Plugin) wago.CallerHostCallFunc {
	handler := b.handler
	return func(caller wago.Caller, call wago.HostCall) {
		state, code := e.stateFor(caller)
		if code != wasiOK {
			setStateError(call.ResultSlots(), code)
			return
		}
		defer state.mu.Unlock()
		current := *e
		current.fs = state
		handler.call(&current, caller, call.ParamSlots(), call.ResultSlots())
	}
}

type handlerID uint8

const (
	dispatchfdWrite handlerID = iota
	dispatchfdRead
	dispatchfdClose
	dispatchfdSeek
	dispatchfdFdstatGet
	dispatchfdPrestatGet
	dispatchfdPrestatDirName
	dispatchprocExit
	dispatchargsSizesGet
	dispatchargsGet
	dispatchenvironSizesGet
	dispatchenvironGet
	dispatchclockTimeGet
	dispatchclockResGet
	dispatchrandomGet
	dispatchschedYield
	dispatchfdAdvise
	dispatchfdAllocate
	dispatchfdDatasync
	dispatchfdSync
	dispatchfdFdstatSetFlags
	dispatchfdFdstatSetRights
	dispatchfdFilestatGet
	dispatchfdFilestatSetSize
	dispatchfdFilestatSetTimes
	dispatchfdPread
	dispatchfdPwrite
	dispatchfdReaddir
	dispatchfdRenumber
	dispatchfdTell
	dispatchpathCreateDirectory
	dispatchpathFilestatGet
	dispatchpathFilestatSetTimes
	dispatchpathLink
	dispatchpathOpen
	dispatchpathReadlink
	dispatchpathRemoveDirectory
	dispatchpathRename
	dispatchpathSymlink
	dispatchpathUnlinkFile
	dispatchpollOneoff
	dispatchprocRaise
	dispatchsockAccept
	dispatchsockRecv
	dispatchsockSend
	dispatchsockShutdown
)

func (h handlerID) call(e *Plugin, m wago.HostModule, p, r []uint64) {
	switch h {
	case dispatchfdWrite:
		e.fdWrite(m, p, r)
	case dispatchfdRead:
		e.fdRead(m, p, r)
	case dispatchfdClose:
		e.fdClose(m, p, r)
	case dispatchfdSeek:
		e.fdSeek(m, p, r)
	case dispatchfdFdstatGet:
		e.fdFdstatGet(m, p, r)
	case dispatchfdPrestatGet:
		e.fdPrestatGet(m, p, r)
	case dispatchfdPrestatDirName:
		e.fdPrestatDirName(m, p, r)
	case dispatchprocExit:
		e.procExit(m, p, r)
	case dispatchargsSizesGet:
		e.argsSizesGet(m, p, r)
	case dispatchargsGet:
		e.argsGet(m, p, r)
	case dispatchenvironSizesGet:
		e.environSizesGet(m, p, r)
	case dispatchenvironGet:
		e.environGet(m, p, r)
	case dispatchclockTimeGet:
		e.clockTimeGet(m, p, r)
	case dispatchclockResGet:
		e.clockResGet(m, p, r)
	case dispatchrandomGet:
		e.randomGet(m, p, r)
	case dispatchschedYield:
		e.schedYield(m, p, r)
	case dispatchfdAdvise:
		e.fdAdvise(m, p, r)
	case dispatchfdAllocate:
		e.fdAllocate(m, p, r)
	case dispatchfdDatasync:
		e.fdDatasync(m, p, r)
	case dispatchfdSync:
		e.fdSync(m, p, r)
	case dispatchfdFdstatSetFlags:
		e.fdFdstatSetFlags(m, p, r)
	case dispatchfdFdstatSetRights:
		e.fdFdstatSetRights(m, p, r)
	case dispatchfdFilestatGet:
		e.fdFilestatGet(m, p, r)
	case dispatchfdFilestatSetSize:
		e.fdFilestatSetSize(m, p, r)
	case dispatchfdFilestatSetTimes:
		e.fdFilestatSetTimes(m, p, r)
	case dispatchfdPread:
		e.fdPread(m, p, r)
	case dispatchfdPwrite:
		e.fdPwrite(m, p, r)
	case dispatchfdReaddir:
		e.fdReaddir(m, p, r)
	case dispatchfdRenumber:
		e.fdRenumber(m, p, r)
	case dispatchfdTell:
		e.fdTell(m, p, r)
	case dispatchpathCreateDirectory:
		e.pathCreateDirectory(m, p, r)
	case dispatchpathFilestatGet:
		e.pathFilestatGet(m, p, r)
	case dispatchpathFilestatSetTimes:
		e.pathFilestatSetTimes(m, p, r)
	case dispatchpathLink:
		e.pathLink(m, p, r)
	case dispatchpathOpen:
		e.pathOpen(m, p, r)
	case dispatchpathReadlink:
		e.pathReadlink(m, p, r)
	case dispatchpathRemoveDirectory:
		e.pathRemoveDirectory(m, p, r)
	case dispatchpathRename:
		e.pathRename(m, p, r)
	case dispatchpathSymlink:
		e.pathSymlink(m, p, r)
	case dispatchpathUnlinkFile:
		e.pathUnlinkFile(m, p, r)
	case dispatchpollOneoff:
		e.pollOneoff(m, p, r)
	case dispatchprocRaise:
		e.procRaise(m, p, r)
	case dispatchsockAccept:
		e.sockAccept(m, p, r)
	case dispatchsockRecv:
		e.sockRecv(m, p, r)
	case dispatchsockSend:
		e.sockSend(m, p, r)
	case dispatchsockShutdown:
		e.sockShutdown(m, p, r)
	default:
		panic("invalid WASI handler")
	}
}

type guestCapability struct {
	cap  wago.Capability
	docs string
}

var guestCapabilities = []guestCapability{
	{CapFDRead, "read streams and granted file descriptors"},
	{CapFDWrite, "write streams and granted file descriptors"},
	{CapFDManage, "close, seek, inspect, and renumber descriptors"},
	{CapPathRead, "inspect paths below configured preopens"},
	{CapPathOpen, "open paths below configured preopens with descriptor rights enforced by the mount"},
	{CapPathWrite, "mutate paths below configured preopens"},
	{CapArgumentsRead, "read runtime-scoped guest argv"},
	{CapEnvironmentRead, "read the configured guest environment"},
	{CapClockRead, "read host clocks"},
	{CapRandomRead, "read cryptographic host randomness"},
	{CapProcessExit, "terminate guest execution with a status code"},
	{CapPoll, "wait for descriptor and clock events"},
	{CapSchedulerYield, "yield guest execution"},
	{CapUnsupported, "call unsupported Preview 1 compatibility stubs"},
}

var importBindings = func() []binding {
	i32 := []wago.ValType{wago.ValI32}
	i32x2 := []wago.ValType{wago.ValI32, wago.ValI32}
	i32x3 := []wago.ValType{wago.ValI32, wago.ValI32, wago.ValI32}
	i32x4 := []wago.ValType{wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32}
	i64 := wago.ValI64
	i32v := wago.ValI32

	return []binding{
		{"fd_write", dispatchfdWrite, i32x4, i32, CapFDWrite, "write iovecs to a file descriptor (stdout/stderr)"},
		{"fd_read", dispatchfdRead, i32x4, i32, CapFDRead, "read into iovecs from a file descriptor (stdin)"},
		{"fd_close", dispatchfdClose, i32, i32, CapFDManage, "close a file descriptor (streams: no-op)"},
		{"fd_seek", dispatchfdSeek, []wago.ValType{i32v, i64, i32v, i32v}, i32, CapFDManage, "seek a file descriptor (streams: ESPIPE)"},
		{"fd_fdstat_get", dispatchfdFdstatGet, i32x2, i32, CapFDManage, "report fd stat (streams: character device)"},
		{"fd_prestat_get", dispatchfdPrestatGet, i32x2, i32, CapFDManage, "report a preopen (none: EBADF)"},
		{"fd_prestat_dir_name", dispatchfdPrestatDirName, i32x3, i32, CapFDManage, "report a preopen dir name (none: EBADF)"},
		{"proc_exit", dispatchprocExit, i32, nil, CapProcessExit, "terminate the program with an exit code"},
		{"args_sizes_get", dispatchargsSizesGet, i32x2, i32, CapArgumentsRead, "report argc and argv byte size"},
		{"args_get", dispatchargsGet, i32x2, i32, CapArgumentsRead, "write argv pointers and bytes"},
		{"environ_sizes_get", dispatchenvironSizesGet, i32x2, i32, CapEnvironmentRead, "report environ count and byte size"},
		{"environ_get", dispatchenvironGet, i32x2, i32, CapEnvironmentRead, "write environ pointers and bytes"},
		{"clock_time_get", dispatchclockTimeGet, []wago.ValType{i32v, i64, i32v}, i32, CapClockRead, "read a clock's current time"},
		{"clock_res_get", dispatchclockResGet, i32x2, i32, CapClockRead, "read a clock's resolution"},
		{"random_get", dispatchrandomGet, i32x2, i32, CapRandomRead, "fill a buffer with random bytes"},

		{"sched_yield", dispatchschedYield, nil, i32, CapSchedulerYield, "yield execution"},
		{"fd_advise", dispatchfdAdvise, []wago.ValType{i32v, i64, i64, i32v}, i32, CapFDManage, "provide file access advice"},
		{"fd_allocate", dispatchfdAllocate, []wago.ValType{i32v, i64, i64}, i32, CapFDWrite, "allocate file space"},
		{"fd_datasync", dispatchfdDatasync, i32, i32, CapFDWrite, "synchronize file data"},
		{"fd_sync", dispatchfdSync, i32, i32, CapFDWrite, "synchronize a file"},
		{"fd_fdstat_set_flags", dispatchfdFdstatSetFlags, i32x2, i32, CapFDManage, "set descriptor flags"},
		{"fd_fdstat_set_rights", dispatchfdFdstatSetRights, []wago.ValType{i32v, i64, i64}, i32, CapFDManage, "reduce descriptor rights"},
		{"fd_filestat_get", dispatchfdFilestatGet, i32x2, i32, CapFDRead, "get file metadata"},
		{"fd_filestat_set_size", dispatchfdFilestatSetSize, []wago.ValType{i32v, i64}, i32, CapFDWrite, "set file size"},
		{"fd_filestat_set_times", dispatchfdFilestatSetTimes, []wago.ValType{i32v, i64, i64, i32v}, i32, CapFDWrite, "set file timestamps"},
		{"fd_pread", dispatchfdPread, []wago.ValType{i32v, i32v, i32v, i64, i32v}, i32, CapFDRead, "read at an offset"},
		{"fd_pwrite", dispatchfdPwrite, []wago.ValType{i32v, i32v, i32v, i64, i32v}, i32, CapFDWrite, "write at an offset"},
		{"fd_readdir", dispatchfdReaddir, []wago.ValType{i32v, i32v, i32v, i64, i32v}, i32, CapFDRead, "read directory entries"},
		{"fd_renumber", dispatchfdRenumber, i32x2, i32, CapFDManage, "renumber a descriptor"},
		{"fd_tell", dispatchfdTell, i32x2, i32, CapFDManage, "get a descriptor offset"},
		{"path_create_directory", dispatchpathCreateDirectory, i32x3, i32, CapPathWrite, "create a directory"},
		{"path_filestat_get", dispatchpathFilestatGet, []wago.ValType{i32v, i32v, i32v, i32v, i32v}, i32, CapPathRead, "get path metadata"},
		{"path_filestat_set_times", dispatchpathFilestatSetTimes, []wago.ValType{i32v, i32v, i32v, i32v, i64, i64, i32v}, i32, CapPathWrite, "set path timestamps"},
		{"path_link", dispatchpathLink, []wago.ValType{i32v, i32v, i32v, i32v, i32v, i32v, i32v}, i32, CapPathWrite, "create a hard link"},
		{"path_open", dispatchpathOpen, []wago.ValType{i32v, i32v, i32v, i32v, i32v, i64, i64, i32v, i32v}, i32, CapPathOpen, "open a path with rights limited by its preopen"},
		{"path_readlink", dispatchpathReadlink, []wago.ValType{i32v, i32v, i32v, i32v, i32v, i32v}, i32, CapPathRead, "read a symbolic link"},
		{"path_remove_directory", dispatchpathRemoveDirectory, i32x3, i32, CapPathWrite, "remove a directory"},
		{"path_rename", dispatchpathRename, []wago.ValType{i32v, i32v, i32v, i32v, i32v, i32v}, i32, CapPathWrite, "rename a path"},
		{"path_symlink", dispatchpathSymlink, []wago.ValType{i32v, i32v, i32v, i32v, i32v}, i32, CapPathWrite, "create a symbolic link"},
		{"path_unlink_file", dispatchpathUnlinkFile, i32x3, i32, CapPathWrite, "unlink a file"},
		{"poll_oneoff", dispatchpollOneoff, i32x4, i32, CapPoll, "wait for events"},
		{"proc_raise", dispatchprocRaise, i32, i32, CapUnsupported, "raise a signal (unsupported)"},
		{"sock_accept", dispatchsockAccept, i32x3, i32, CapUnsupported, "accept a socket (unsupported)"},
		{"sock_recv", dispatchsockRecv, []wago.ValType{i32v, i32v, i32v, i32v, i32v, i32v}, i32, CapUnsupported, "receive from a socket (unsupported)"},
		{"sock_send", dispatchsockSend, []wago.ValType{i32v, i32v, i32v, i32v, i32v}, i32, CapUnsupported, "send to a socket (unsupported)"},
		{"sock_shutdown", dispatchsockShutdown, i32x2, i32, CapUnsupported, "shut down a socket (unsupported)"},
	}
}()

func validatePluginConfig(raw json.RawMessage) error {
	_, err := decodePluginConfig(raw)
	return err
}

func decodePluginConfig(raw json.RawMessage) (pluginConfig, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > maxConfigBytes {
		return pluginConfig{}, fmt.Errorf("wasi: config exceeds %d bytes", maxConfigBytes)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return pluginConfig{}, fmt.Errorf("wasi: config must be a JSON object")
	}
	if !utf8.Valid(trimmed) {
		return pluginConfig{}, fmt.Errorf("wasi: config is not valid UTF-8")
	}
	if err := rejectDuplicateJSONKeys(trimmed); err != nil {
		return pluginConfig{}, fmt.Errorf("wasi: config: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return pluginConfig{}, fmt.Errorf("wasi: config: %w", err)
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return pluginConfig{}, fmt.Errorf("wasi: config field %q must not be null", name)
		}
	}
	var cfg pluginConfig
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return pluginConfig{}, fmt.Errorf("wasi: config: %w", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return pluginConfig{}, fmt.Errorf("wasi: config has a trailing JSON value")
	}
	if _, err := configFromPluginConfig(cfg); err != nil {
		return pluginConfig{}, err
	}
	return cfg, nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value func() error
	value = func() error {
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate object key %q", key)
				}
				seen[key] = struct{}{}
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		wantEnd := json.Delim('}')
		if delim == '[' {
			wantEnd = ']'
		}
		if end != wantEnd {
			return fmt.Errorf("mismatched JSON delimiter %q", end)
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}

func configFromPluginConfig(cfg pluginConfig) (Config, error) {
	resolved := Config{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Clocks: newSystemClock(nil), Context: context.Background(),
		MaxOpenFiles: 1024, MaxIOVecs: 1024, MaxSubscriptionsPerPoll: 1024,
	}
	if err := applyInputMode(&resolved, cfg.Stdin); err != nil {
		return Config{}, err
	}
	if err := applyOutputMode("stdout", &resolved.Stdout, cfg.Stdout); err != nil {
		return Config{}, err
	}
	if err := applyOutputMode("stderr", &resolved.Stderr, cfg.Stderr); err != nil {
		return Config{}, err
	}
	if cfg.Env != nil {
		if len(*cfg.Env) > 4096 {
			return Config{}, fmt.Errorf("wasi: env has %d entries, max 4096", len(*cfg.Env))
		}
		resolved.Env = append([]string(nil), (*cfg.Env)...)
		for _, entry := range resolved.Env {
			name, _, ok := strings.Cut(entry, "=")
			if !ok || name == "" || strings.ContainsRune(entry, 0) || len(entry) > 32768 {
				return Config{}, fmt.Errorf("wasi: invalid environment entry %q", entry)
			}
		}
	}
	if cfg.Mounts != nil {
		if len(*cfg.Mounts) > 64 {
			return Config{}, fmt.Errorf("wasi: mounts has %d entries, max 64", len(*cfg.Mounts))
		}
		seen := make(map[string]struct{}, len(*cfg.Mounts))
		for _, mount := range *cfg.Mounts {
			if err := validateMount(mount); err != nil {
				return Config{}, err
			}
			if _, ok := seen[mount.GuestPath]; ok {
				return Config{}, fmt.Errorf("wasi: duplicate guest mount path %q", mount.GuestPath)
			}
			seen[mount.GuestPath] = struct{}{}
		}
		resolved.Mounts = append([]Preopen(nil), (*cfg.Mounts)...)
	}
	if cfg.MaxOpenFiles != nil {
		if *cfg.MaxOpenFiles < 3 || *cfg.MaxOpenFiles > 65536 {
			return Config{}, fmt.Errorf("wasi: maxOpenFiles must be between 3 and 65536")
		}
		resolved.MaxOpenFiles = *cfg.MaxOpenFiles
	}
	if cfg.MaxIOVecs != nil {
		if *cfg.MaxIOVecs < 1 || *cfg.MaxIOVecs > 65536 {
			return Config{}, fmt.Errorf("wasi: maxIOVecs must be between 1 and 65536")
		}
		resolved.MaxIOVecs = *cfg.MaxIOVecs
	}
	if cfg.MaxSubscriptions != nil {
		if *cfg.MaxSubscriptions < 1 || *cfg.MaxSubscriptions > 65536 {
			return Config{}, fmt.Errorf("wasi: maxSubscriptionsPerPoll must be between 1 and 65536")
		}
		resolved.MaxSubscriptionsPerPoll = *cfg.MaxSubscriptions
	}
	return resolved, nil
}

func applyInputMode(cfg *Config, mode *string) error {
	if mode == nil || *mode == "inherit" {
		return nil
	}
	if *mode == "eof" {
		cfg.Stdin = nil
		return nil
	}
	return fmt.Errorf("wasi: unsupported stdin mode %q", *mode)
}

func applyOutputMode(name string, dst *io.Writer, mode *string) error {
	if mode == nil || *mode == "inherit" {
		return nil
	}
	if *mode == "discard" {
		*dst = io.Discard
		return nil
	}
	return fmt.Errorf("wasi: unsupported %s mode %q", name, *mode)
}

func cloneConfig(cfg Config) Config {
	cfg.Args = append([]string(nil), cfg.Args...)
	cfg.Env = append([]string(nil), cfg.Env...)
	cfg.Mounts = append([]Preopen(nil), cfg.Mounts...)
	if cfg.Clocks == nil {
		cfg.Clocks = newSystemClock(nil)
	}
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	if cfg.MaxIOVecs == 0 {
		cfg.MaxIOVecs = 1024
	}
	if cfg.MaxSubscriptionsPerPoll == 0 {
		cfg.MaxSubscriptionsPerPoll = 1024
	}
	return cfg
}

func validateMount(mount Preopen) error {
	guest, host := mount.GuestPath, mount.HostPath
	if len(guest) == 0 || len(guest) > 4096 || !strings.HasPrefix(guest, "/") || path.Clean(guest) != guest || strings.ContainsRune(guest, 0) {
		return fmt.Errorf("wasi: invalid guest mount path %q", guest)
	}
	if len(host) == 0 || len(host) > 4096 || !filepath.IsAbs(host) || filepath.Clean(host) != host || strings.ContainsRune(host, 0) {
		return fmt.Errorf("wasi: mount %q requires a clean absolute host path", guest)
	}
	return nil
}

// --- memory helpers (bounds-checked; malformed pointers yield EFAULT, never a
// Go panic that would abort the whole instance) ---

func le32(mem []byte, off uint32) (uint32, bool) {
	if int(off)+4 > len(mem) {
		return 0, false
	}
	return binary.LittleEndian.Uint32(mem[off:]), true
}

func putLe32(mem []byte, off, v uint32) bool {
	if int(off)+4 > len(mem) {
		return false
	}
	binary.LittleEndian.PutUint32(mem[off:], v)
	return true
}

func putLe64(mem []byte, off uint32, v uint64) bool {
	if int(off)+8 > len(mem) {
		return false
	}
	binary.LittleEndian.PutUint64(mem[off:], v)
	return true
}

// --- fd_* ---

func (e *Plugin) fdWrite(m wago.HostModule, p, r []uint64) {
	fd, iovs, n, nwrittenPtr := uint32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
	f, code := e.entry(fd)
	if code == 0 {
		code = require(f, rightFDWrite)
	}
	if code != 0 {
		r[0] = code
		return
	}
	out := f.writer
	if f.file != nil {
		out = f.file
	}
	mem := m.Memory()
	bufs, code := e.iovecs(mem, iovs, n)
	if code != 0 {
		r[0] = code
		return
	}
	var total uint32
	var writeErr error
	for _, buf := range bufs {
		if out != nil {
			nn, err := out.Write(buf)
			if nn < 0 || nn > len(buf) {
				err = io.ErrShortWrite
				nn = 0
			}
			total += uint32(nn)
			if err != nil || nn != len(buf) {
				if err == nil {
					err = io.ErrShortWrite
				}
				writeErr = err
				break
			}
		} else {
			total += uint32(len(buf))
		}
	}
	if !putLe32(mem, nwrittenPtr, total) {
		r[0] = wasiEFault
		return
	}
	if writeErr != nil && total == 0 {
		r[0] = errno(writeErr)
		return
	}
	r[0] = wasiOK
}

func (e *Plugin) fdRead(m wago.HostModule, p, r []uint64) {
	fd, iovs, n, nreadPtr := uint32(p[0]), uint32(p[1]), uint32(p[2]), uint32(p[3])
	f, code := e.entry(fd)
	if code == 0 {
		code = require(f, rightFDRead)
	}
	if code != 0 {
		r[0] = code
		return
	}
	in := f.reader
	if f.file != nil {
		in = f.file
	}
	mem := m.Memory()
	bufs, code := e.iovecs(mem, iovs, n)
	if code != 0 {
		r[0] = code
		return
	}
	var total uint32
	var readErr error
	if in != nil {
		for _, buf := range bufs {
			nn, err := in.Read(buf)
			if nn < 0 || nn > len(buf) {
				err = io.ErrNoProgress
				nn = 0
			}
			total += uint32(nn)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					readErr = err
				}
				break
			}
			if nn < len(buf) {
				break
			}
		}
	}
	if !putLe32(mem, nreadPtr, total) {
		r[0] = wasiEFault
		return
	}
	if readErr != nil && total == 0 {
		r[0] = errno(readErr)
		return
	}
	r[0] = wasiOK
}

func (e *Plugin) fdClose(_ wago.HostModule, p, r []uint64) {
	fd := uint32(p[0])
	f, code := e.entry(fd)
	if code == 0 {
		if f.dirIter != nil {
			code = errno(f.dirIter.Close())
			f.dirIter = nil
		}
		if f.file != nil {
			closeCode := errno(f.file.Close())
			if code == 0 {
				code = closeCode
			}
		}
		if code == 0 {
			delete(e.fs.fds, fd)
		}
	}
	r[0] = code
}

func (e *Plugin) fdSeek(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 {
		code = require(f, rightFDSeek)
	}
	if code == 0 && f.file == nil {
		code = wasiESpipe
	}
	if code == 0 {
		if st, err := f.file.Stat(); err != nil {
			code = errno(err)
		} else if st.IsDir() {
			code = wasiEBadf
		}
	}
	whence := int(p[2])
	if code == 0 && (whence < 0 || whence > 2) {
		code = wasiEInval
	}
	if code == 0 {
		off, err := f.file.Seek(int64(p[1]), whence)
		if err != nil {
			code = errno(err)
		} else if !putLe64(m.Memory(), uint32(p[3]), uint64(off)) {
			code = wasiEFault
		}
	}
	r[0] = code
}

func (e *Plugin) fdFdstatGet(m wago.HostModule, p, r []uint64) {
	fd, buf := uint32(p[0]), uint32(p[1])
	f, code := e.entry(fd)
	if code != 0 {
		r[0] = code
		return
	}
	mem := m.Memory()
	if int(buf)+24 > len(mem) {
		r[0] = wasiEFault
		return
	}
	for i := uint32(0); i < 24; i++ {
		mem[buf+i] = 0
	}
	if f.file == nil {
		mem[buf] = filetypeCharacterDevice
	} else if st, err := f.file.Stat(); err != nil {
		r[0] = errno(err)
		return
	} else {
		mem[buf] = filetype(st)
	}
	binary.LittleEndian.PutUint16(mem[buf+2:], f.flags)
	binary.LittleEndian.PutUint64(mem[buf+8:], f.rights)
	binary.LittleEndian.PutUint64(mem[buf+16:], f.inheriting)
	r[0] = wasiOK
}

func (e *Plugin) fdPrestatGet(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 && f.preopen == "" {
		code = wasiEBadf
	}
	if code == 0 {
		mem, ptr := m.Memory(), uint32(p[1])
		if uint64(ptr)+8 > uint64(len(mem)) {
			code = wasiEFault
		} else {
			clear(mem[ptr : ptr+8])
			binary.LittleEndian.PutUint32(mem[ptr+4:], uint32(len(f.preopen)))
		}
	}
	r[0] = code
}

func (e *Plugin) fdPrestatDirName(m wago.HostModule, p, r []uint64) {
	f, code := e.entry(uint32(p[0]))
	if code == 0 && f.preopen == "" {
		code = wasiEBadf
	}
	ptr, n := uint32(p[1]), uint32(p[2])
	if code == 0 && n < uint32(len(f.preopen)) {
		code = wasiENametoolong
	}
	if code == 0 && uint64(ptr)+uint64(len(f.preopen)) > uint64(len(m.Memory())) {
		code = wasiEFault
	}
	if code == 0 {
		copy(m.Memory()[ptr:], f.preopen)
	}
	r[0] = code
}

// --- process / args / env ---

func (e *Plugin) procExit(_ wago.HostModule, p, r []uint64) {
	panic(wago.HostExit{Code: int32(uint32(p[0]))})
}

func (e *Plugin) argsSizesGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeCounts(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Args)
}

func (e *Plugin) argsGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeStrings(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Args)
}

func (e *Plugin) environSizesGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeCounts(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Env)
}

func (e *Plugin) environGet(m wago.HostModule, p, r []uint64) {
	r[0] = writeStrings(m.Memory(), uint32(p[0]), uint32(p[1]), e.cfg.Env)
}

// writeCounts writes the item count and the total NUL-terminated byte size.
func writeCounts(mem []byte, countPtr, sizePtr uint32, items []string) uint64 {
	total := 0
	for _, s := range items {
		total += len(s) + 1
	}
	if !putLe32(mem, countPtr, uint32(len(items))) || !putLe32(mem, sizePtr, uint32(total)) {
		return wasiEFault
	}
	return wasiOK
}

// writeStrings writes the pointer array then the packed NUL-terminated strings.
func writeStrings(mem []byte, ptrArray, buf uint32, items []string) uint64 {
	cur := buf
	for i, s := range items {
		if !putLe32(mem, ptrArray+uint32(i)*4, cur) {
			return wasiEFault
		}
		if int(cur)+len(s)+1 > len(mem) {
			return wasiEFault
		}
		copy(mem[cur:], s)
		mem[cur+uint32(len(s))] = 0
		cur += uint32(len(s)) + 1
	}
	return wasiOK
}

// --- clock / random ---

func (e *Plugin) clockTimeGet(m wago.HostModule, p, r []uint64) {
	if p[0] > 3 {
		r[0] = wasiEInval
		return
	}
	now, _, err := clockValue(e.cfg.Clocks, uint32(p[0]))
	if err != nil {
		r[0] = wasiENotsup
		return
	}
	if !putLe64(m.Memory(), uint32(p[2]), now) {
		r[0] = wasiEFault
		return
	}
	r[0] = wasiOK
}

func (e *Plugin) clockResGet(m wago.HostModule, p, r []uint64) {
	if p[0] > 3 {
		r[0] = wasiEInval
		return
	}
	_, resolution, err := clockValue(e.cfg.Clocks, uint32(p[0]))
	if err != nil {
		r[0] = wasiENotsup
		return
	}
	if !putLe64(m.Memory(), uint32(p[1]), resolution) {
		r[0] = wasiEFault
		return
	}
	r[0] = wasiOK
}

func (e *Plugin) randomGet(m wago.HostModule, p, r []uint64) {
	buf, n := uint32(p[0]), uint32(p[1])
	mem := m.Memory()
	if int(buf)+int(n) > len(mem) {
		r[0] = wasiEFault
		return
	}
	src := e.cfg.Rand
	if src == nil {
		src = rand.Reader
	}
	if _, err := io.ReadFull(src, mem[buf:buf+n]); err != nil {
		r[0] = wasiEIo
		return
	}
	r[0] = wasiOK
}
