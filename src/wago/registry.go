package wago

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/wago-org/wago/internal/jsonstrict"
)

// Registrar is the declarative builder passed to Plugin.Register. It is scoped
// to one immutable definition and reviewed selection. Privileged contributions
// can be made only through exact authority handles.
type Registrar struct {
	definition     PluginDefinition
	selection      PluginSelection
	requests       map[Authority]AuthorityRequest
	grants         map[Authority]AuthorityGrant
	used           map[Authority]struct{}
	sealed         bool
	caps           []capabilitySpec
	imports        []*registeredImport
	importErr      error
	hooks          *hookRegistry
	managers       []*InstanceManager
	activate       []func(*Runtime)
	closeInstances []func() error
	drainInstances []func() error
	revoke         []func() error
	callGate       *pluginCallGate
	provides       []contractProvision
	consumes       []contractBinder
	lifecycle      PluginLifecycle
	hasLifecycle   bool
	config         json.RawMessage
	customTypes    map[string]CustomType
	instructions   []*registeredInstruction
}

func (r *Registrar) recordImportError(module, name string, err error) {
	if r != nil && err != nil {
		r.importErr = errors.Join(r.importErr, fmt.Errorf("host import %q.%q: %w", module, name, err))
	}
}

func newRegistrar(def PluginDefinition, selection PluginSelection) *Registrar {
	r := &Registrar{
		definition: def,
		selection:  selection,
		requests:   make(map[Authority]AuthorityRequest, len(def.Authorities)),
		grants:     make(map[Authority]AuthorityGrant, len(selection.Grants)),
		used:       map[Authority]struct{}{},
		hooks:      &hookRegistry{},
		callGate:   newPluginCallGate(def.ID),
		config:     append(json.RawMessage(nil), selection.Config...),
	}
	for _, request := range def.Authorities {
		r.requests[request.Name] = request
	}
	for _, grant := range selection.Grants {
		r.grants[grant.Name] = grant
	}
	return r
}

func (r *Registrar) ensureOpen() error {
	if r == nil {
		return fmt.Errorf("wago: nil plugin registrar")
	}
	if r.sealed {
		return fmt.Errorf("wago: plugin registrar is sealed")
	}
	return nil
}

// Granted reports whether this exact authority was reviewed and granted. It
// does not apply parent, wildcard, or sub-authority matching.
func (r *Registrar) Granted(authority Authority) bool {
	if r == nil {
		return false
	}
	_, ok := r.grants[authority]
	return ok
}

// Config strictly decodes the plugin's opaque configuration. Unknown fields in
// struct destinations and trailing JSON fail closed. An absent config is {}.
func (r *Registrar) Config(dst any) error {
	if err := r.ensureOpen(); err != nil {
		return err
	}
	b := r.config
	if len(b) == 0 {
		b = []byte("{}")
	}
	if err := jsonstrict.ValidateTypedJSON(b, dst); err != nil {
		return &PluginError{Plugin: r.definition.ID, Phase: PluginPhaseConfigure, Path: "config", Err: err}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &PluginError{Plugin: r.definition.ID, Phase: PluginPhaseConfigure, Path: "config", Err: err}
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &PluginError{Plugin: r.definition.ID, Phase: PluginPhaseConfigure, Path: "config", Err: fmt.Errorf("trailing JSON value")}
	}
	return nil
}

// Lifecycle declares activation and teardown. At most one lifecycle may be
// declared. Stop is invoked for a failed Start as well as normal teardown.
func (r *Registrar) Lifecycle(lifecycle PluginLifecycle) error {
	if err := r.ensureOpen(); err != nil {
		return err
	}
	if r.hasLifecycle {
		return fmt.Errorf("wago: plugin %q declares lifecycle more than once", r.definition.ID)
	}
	r.lifecycle, r.hasLifecycle = lifecycle, true
	return nil
}

// ManagedInstances requests a bounded plugin-owned instance manager.
func (r *Registrar) ManagedInstances() (*InstanceManager, error) {
	grant, err := r.authorize(AuthorityInstanceManage)
	if err != nil {
		return nil, err
	}
	m := newPendingInstanceManager(r.definition.ID, grant.Scope)
	r.managers = append(r.managers, m)
	r.activate = append(r.activate, m.activate)
	r.closeInstances = append(r.closeInstances, m.closeLogical)
	r.drainInstances = append(r.drainInstances, m.drain)
	return m, nil
}

// GuestCapability declares a permission this plugin exposes to guest Wasm.
func (r *Registrar) GuestCapability(cap Capability, opts ...CapabilityOption) error {
	if err := r.ensureOpen(); err != nil {
		return err
	}
	if cap == "" {
		return fmt.Errorf("wago: empty guest capability")
	}
	spec := capabilitySpec{cap: cap}
	for _, opt := range opts {
		opt(&spec)
	}
	r.caps = append(r.caps, spec)
	return nil
}

// capabilitySpec is a guest capability plus optional documentation.
type capabilitySpec struct {
	cap  Capability
	docs string
}

type CapabilityOption func(*capabilitySpec)

func CapabilityDocs(docs string) CapabilityOption {
	return func(s *capabilitySpec) { s.docs = docs }
}

// registeredImport is one declared host function.
type registeredImport struct {
	module          string
	name            string
	fn              any
	eventI32        I32HostEvent
	params          []ValType
	results         []ValType
	inferredParams  []ValType
	inferredResults []ValType
	inferred        bool
	cap             Capability
	hasCap          bool
	docs            string
}

func (i *registeredImport) key() string { return importBindingMapKey(i.module, i.name) }

func (i *registeredImport) displayName() string { return i.module + "." + i.name }

func cloneRegisteredImport(imp *registeredImport) *registeredImport {
	if imp == nil {
		return nil
	}
	clone := *imp
	clone.params = append([]ValType(nil), imp.params...)
	clone.results = append([]ValType(nil), imp.results...)
	clone.inferredParams = append([]ValType(nil), imp.inferredParams...)
	clone.inferredResults = append([]ValType(nil), imp.inferredResults...)
	return &clone
}

type ImportFuncBuilder struct {
	imp     *registeredImport
	imports *Imports
	reg     *Registrar
}

func (f *ImportFuncBuilder) mutable() bool {
	if f == nil || f.imp == nil {
		return false
	}
	if f.reg != nil && f.reg.sealed {
		f.reg.recordImportError(f.imp.module, f.imp.name, fmt.Errorf("plugin registrar is sealed"))
		return false
	}
	return true
}

func (f *ImportFuncBuilder) Params(types ...ValType) *ImportFuncBuilder {
	if f.mutable() {
		if f.imports != nil {
			f.imports.mu.Lock()
			defer f.imports.mu.Unlock()
		}
		if f.imports != nil && f.imports.sealed {
			f.imports.record(fmt.Errorf("wago: import %q.%q: collection is sealed", f.imp.module, f.imp.name))
			return f
		}
		f.imp.params = append(f.imp.params[:0], types...)
	}
	return f
}

func (f *ImportFuncBuilder) Results(types ...ValType) *ImportFuncBuilder {
	if f.mutable() {
		if f.imports != nil {
			f.imports.mu.Lock()
			defer f.imports.mu.Unlock()
		}
		if f.imports != nil && f.imports.sealed {
			f.imports.record(fmt.Errorf("wago: import %q.%q: collection is sealed", f.imp.module, f.imp.name))
			return f
		}
		f.imp.results = append(f.imp.results[:0], types...)
	}
	return f
}

func (f *ImportFuncBuilder) Capability(cap Capability) *ImportFuncBuilder {
	if f.mutable() {
		if f.imports != nil {
			f.imports.mu.Lock()
			defer f.imports.mu.Unlock()
		}
		if f.imports != nil && f.imports.sealed {
			f.imports.record(fmt.Errorf("wago: import %q.%q: collection is sealed", f.imp.module, f.imp.name))
			return f
		}
		f.imp.cap, f.imp.hasCap = cap, true
	}
	return f
}

func (f *ImportFuncBuilder) Docs(docs string) *ImportFuncBuilder {
	if f.mutable() {
		if f.imports != nil {
			f.imports.mu.Lock()
			defer f.imports.mu.Unlock()
		}
		if f.imports != nil && f.imports.sealed {
			f.imports.record(fmt.Errorf("wago: import %q.%q: collection is sealed", f.imp.module, f.imp.name))
			return f
		}
		f.imp.docs = docs
	}
	return f
}
