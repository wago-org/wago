package wago

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/tests/wasmtest"
)

type pluginFunc func(*Registrar) error

func (f pluginFunc) Register(r *Registrar) error { return f(r) }

func testDefinition(id string) PluginDefinition {
	return PluginDefinition{ID: id, Version: "1.0.0", Provenance: PluginProvenance{Repository: "https://example.com/" + id, License: "MIT"}}
}

func testSet(t testing.TB, providers ...PluginProvider) PluginSet {
	t.Helper()
	set := PluginSet{Providers: providers}
	for _, provider := range providers {
		digest, err := DefinitionDigest(provider.Definition)
		if err != nil {
			t.Fatal(err)
		}
		selection := PluginSelection{
			ID: provider.Definition.ID, DefinitionDigest: digest, Direct: true,
			Dependencies: map[string]string{},
		}
		for _, requirement := range provider.Definition.Requires {
			selection.Dependencies[requirement.ID] = requirement.Version
		}
		for _, req := range provider.Definition.Authorities {
			if req.Mode == AuthorityRequired {
				selection.Grants = append(selection.Grants, AuthorityGrant{Name: req.Name, Scope: req.Scope})
			}
		}
		for _, consume := range provider.Definition.Consumes {
			var owners []string
			for _, candidate := range providers {
				for _, provided := range candidate.Definition.Provides {
					if provided.ID == consume.ID && provided.Major == consume.Major {
						owners = append(owners, candidate.Definition.ID)
					}
				}
			}
			sort.Strings(owners)
			selection.Contracts = append(selection.Contracts, ContractBinding{ID: consume.ID, Major: consume.Major, Providers: owners})
		}
		set.Selections = append(set.Selections, selection)
	}
	return set
}

func TestDefinitionDigestCanonicalAndStrict(t *testing.T) {
	a := testDefinition("example.com/plugin")
	a.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "imports", Scope: AuthorityScope{Modules: []string{"b", "a"}}}}
	a.ConfigSchema = []byte(`{"type":"object","additionalProperties":false,"properties":{"z":{"type":"string"},"a":{"type":"boolean"}}}`)
	b := a
	b.Authorities[0].Scope.Modules = []string{"a", "b"}
	b.ConfigSchema = []byte(` { "properties": {"a": {"type": "boolean"}, "z": {"type": "string"}}, "additionalProperties": false, "type": "object" } `)
	da, err := DefinitionDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := DefinitionDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db || !strings.HasPrefix(da, "sha256:") {
		t.Fatalf("digests %q %q", da, db)
	}
	for _, raw := range []string{`{} garbage`, `{} {}`} {
		broken := a
		broken.ConfigSchema = []byte(raw)
		if _, err := DefinitionDigest(broken); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	dup := a
	dup.Authorities[0].Scope.Modules = []string{"a", "a"}
	if _, err := DefinitionDigest(dup); err == nil {
		t.Fatal("accepted duplicate module")
	}

	for name, raw := range map[string]string{
		"array":                    `[]`,
		"open object":              `{"type":"object"}`,
		"wrong root type":          `{"type":"array","additionalProperties":false}`,
		"unsupported schema draft": `{"$schema":"https://json-schema.org/draft/2019-09/schema","type":"object","additionalProperties":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			broken := a
			broken.ConfigSchema = []byte(raw)
			if _, err := DefinitionDigest(broken); err == nil {
				t.Fatalf("accepted config schema %s", raw)
			}
		})
	}

	emptyConstraint := a
	emptyConstraint.Requires = []PluginRequirement{{ID: "example.com/dependency"}}
	if _, err := DefinitionDigest(emptyConstraint); err == nil {
		t.Fatal("accepted empty dependency constraint")
	}
}

func TestDefinitionValidationMatchesPublishedCatalogBoundary(t *testing.T) {
	valid := testDefinition("example.com/catalog-boundary")
	valid.Stability = Stable
	valid.Compatibility = Compatibility{Engines: map[string]string{"wago": "^0.1.0"}, Platforms: []string{"linux/amd64"}}
	valid.Provenance.Homepage = "https://example.com/plugin#readme"
	valid.Provenance.Authors = []string{"Acme"}
	valid.Authorities = []AuthorityRequest{{
		Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "define exact imports",
		Scope: AuthorityScope{Modules: []string{"env"}},
	}}
	if _, err := DefinitionDigest(valid); err != nil {
		t.Fatalf("valid published definition: %v", err)
	}

	for name, mutate := range map[string]func(*PluginDefinition){
		"uppercase version prefix": func(def *PluginDefinition) { def.Version = "V1.0.0" },
		"unknown stability":        func(def *PluginDefinition) { def.Stability = "preview" },
		"invalid engine slug":      func(def *PluginDefinition) { def.Compatibility.Engines = map[string]string{"Wago": "*"} },
		"invalid platform":         func(def *PluginDefinition) { def.Compatibility.Platforms = []string{"linux"} },
		"repository credentials":   func(def *PluginDefinition) { def.Provenance.Repository = "https://user@example.com/plugin" },
		"homepage credentials":     func(def *PluginDefinition) { def.Provenance.Homepage = "https://user@example.com/plugin" },
		"blank license":            func(def *PluginDefinition) { def.Provenance.License = " " },
		"padded author":            func(def *PluginDefinition) { def.Provenance.Authors = []string{" Acme "} },
		"padded authority module": func(def *PluginDefinition) {
			def.Authorities[0].Scope.Modules = []string{" env "}
		},
	} {
		t.Run(name, func(t *testing.T) {
			broken := valid
			broken.Compatibility.Engines = cloneStringMap(valid.Compatibility.Engines)
			broken.Compatibility.Platforms = append([]string(nil), valid.Compatibility.Platforms...)
			broken.Provenance.Authors = append([]string(nil), valid.Provenance.Authors...)
			broken.Authorities = append([]AuthorityRequest(nil), valid.Authorities...)
			broken.Authorities[0].Scope.Modules = append([]string(nil), valid.Authorities[0].Scope.Modules...)
			mutate(&broken)
			if _, err := DefinitionDigest(broken); err == nil {
				t.Fatal("invalid published definition was accepted")
			}
		})
	}
}

func TestCanonicalPluginIDGrammar(t *testing.T) {
	accepted := []string{
		"github.com/wago-org/plugin",
		"Example.COM/Plugin_2",
		"sub-domain.example.io/a/b.c_d~e-f",
		"1.example/2",
	}
	rejected := []string{
		"example/plugin", "example.com", ".example.com/plugin", "example..com/plugin",
		"-example.com/plugin", "example-.com/plugin", "example.com/-plugin", "example.com/plugin-",
		"example.com/.plugin", "example.com/plugin.", "example.com/a//b", "example.com/a@b",
		"example.com/a+b", "example.com/a:b", "example.com/a?b", "example.com/a b",
		"example.com/plugïn", "https://example.com/plugin",
	}
	for _, id := range accepted {
		if !validCanonicalPath(id) {
			t.Errorf("rejected valid ID %q", id)
		}
	}
	for _, id := range rejected {
		if validCanonicalPath(id) {
			t.Errorf("accepted invalid ID %q", id)
		}
	}
}

func TestDigestMismatchBeforeFactory(t *testing.T) {
	called := false
	def := testDefinition("example.com/mismatch")
	set := PluginSet{Providers: []PluginProvider{{Definition: def, New: func() Plugin { called = true; return pluginFunc(nil) }}}, Selections: []PluginSelection{{ID: def.ID, DefinitionDigest: "sha256:wrong", Direct: true, Dependencies: map[string]string{}}}}
	if err := NewRuntime().LoadPlugins(context.Background(), set); err == nil {
		t.Fatal("accepted mismatch")
	}
	if called {
		t.Fatal("factory ran")
	}
}

func TestAuthorityExactGrantsAndHostScope(t *testing.T) {
	def := testDefinition("example.com/authority")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "env", Scope: AuthorityScope{Modules: []string{"env"}}}}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			host, err := r.HostImports()
			if err != nil {
				return err
			}
			if _, err := host.Module("other"); !errors.Is(err, ErrPermissionDenied) {
				return fmt.Errorf("out-of-scope=%v", err)
			}
			module, err := host.Module("env")
			if err != nil {
				return err
			}
			module.Func("f", func(HostModule, []uint64, []uint64) {})
			return nil
		})
	}}
	set := testSet(t, provider)
	if err := NewRuntime().LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	set.Selections[0].Grants = nil
	if err := NewRuntime().LoadPlugins(context.Background(), set); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("missing grant=%v", err)
	}
	set = testSet(t, provider)
	set.Selections[0].Grants[0].Name = AuthorityHostCallerIdentify
	if err := NewRuntime().LoadPlugins(context.Background(), set); err == nil {
		t.Fatal("accepted wrong authority")
	}
	set = testSet(t, provider)
	set.Selections[0].Grants[0].Scope.Modules = []string{"env", "other"}
	if err := NewRuntime().LoadPlugins(context.Background(), set); err == nil {
		t.Fatal("accepted widened scope")
	}
}

func TestInspectIsSideEffectFreeAndCommitAtomic(t *testing.T) {
	factories := 0
	def := testDefinition("example.com/inspect")
	provider := PluginProvider{Definition: def, New: func() Plugin { factories++; return pluginFunc(nil) }}
	set := testSet(t, provider)
	if _, err := InspectPluginPlan(set); err != nil {
		t.Fatal(err)
	}
	if factories != 0 {
		t.Fatal("inspection ran factory")
	}
	a := testDefinition("example.com/a")
	a.Authorities = []AuthorityRequest{{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "x", Scope: AuthorityScope{Modules: []string{"env"}}}}
	b := testDefinition("example.com/b")
	b.Authorities = a.Authorities
	mk := func(def PluginDefinition) PluginProvider {
		return PluginProvider{Definition: def, New: func() Plugin {
			return pluginFunc(func(r *Registrar) error {
				h, _ := r.HostImports()
				m, _ := h.Module("env")
				m.Func(def.ID, func(HostModule, []uint64, []uint64) {})
				return nil
			})
		}}
	}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, mk(a), mk(b))); !errors.Is(err, ErrPluginConflict) {
		t.Fatalf("collision=%v", err)
	}
	if len(rt.Plugins()) != 0 || len(rt.imports) != 0 {
		t.Fatal("partial commit")
	}
}

func TestDependencyAndContractCycles(t *testing.T) {
	a, b := testDefinition("example.com/a"), testDefinition("example.com/b")
	a.Requires = []PluginRequirement{{ID: b.ID, Version: "^1.0.0"}}
	b.Requires = []PluginRequirement{{ID: a.ID, Version: "^1.0.0"}}
	noop := func(def PluginDefinition) PluginProvider {
		return PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(nil) }}
	}
	if _, err := InspectPluginPlan(testSet(t, noop(a), noop(b))); err == nil {
		t.Fatal("accepted explicit cycle")
	}
	a, b = testDefinition("example.com/a"), testDefinition("example.com/b")
	a.Provides = []ContractSpec{{ID: "example.com/a", Major: 1}}
	a.Consumes = []ContractRequirement{{ID: "example.com/b", Major: 1, Mode: ContractRequired}}
	b.Provides = []ContractSpec{{ID: "example.com/b", Major: 1}}
	b.Consumes = []ContractRequirement{{ID: "example.com/a", Major: 1, Mode: ContractRequired}}
	pa := PluginProvider{Definition: a, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			ProvideContract(r, a.Provides[0], 1)
			_, e := RequireContract(r, ContractSpec{ID: "example.com/b", Major: 1}, ContractRequired, (*int)(nil))
			return e
		})
	}}
	pb := PluginProvider{Definition: b, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			ProvideContract(r, b.Provides[0], 1)
			_, e := RequireContract(r, ContractSpec{ID: "example.com/a", Major: 1}, ContractRequired, (*int)(nil))
			return e
		})
	}}
	if _, err := InspectPluginPlan(testSet(t, pa, pb)); err == nil {
		t.Fatal("accepted contract cycle")
	}
}

func TestContractsRequiredOptionalManyVersionAndRevocation(t *testing.T) {
	spec := ContractSpec{ID: "example.com/counter", Major: 1}
	p1 := testDefinition("example.com/p1")
	p1.Provides = []ContractSpec{spec}
	p2 := testDefinition("example.com/p2")
	p2.Provides = []ContractSpec{spec}
	consumer := testDefinition("example.com/consumer")
	consumer.Consumes = []ContractRequirement{{ID: spec.ID, Major: spec.Major, Mode: ContractMany}}
	var ref *ContractRef
	mkProvider := func(def PluginDefinition, v int) PluginProvider {
		return PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(func(r *Registrar) error { return ProvideContract(r, spec, v) }) }}
	}
	cp := PluginProvider{Definition: consumer, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			var err error
			ref, err = RequireContract(r, spec, ContractMany, (*int)(nil))
			return err
		})
	}}
	set := testSet(t, mkProvider(p1, 1), mkProvider(p2, 2), cp)
	if err := NewRuntime().LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	var got []int
	if err := ref.CallAll(func(values []any) error {
		for _, v := range values {
			got = append(got, v.(int))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("many=%v", got)
	}
	wrong := consumer
	wrong.Consumes[0].Major = 2
	wrong.Consumes[0].Mode = ContractRequired
	if _, err := InspectPluginPlan(testSet(t, mkProvider(p1, 1), PluginProvider{Definition: wrong, New: cp.New})); err == nil {
		t.Fatal("accepted version mismatch")
	}
}

func TestContractBindingsChooseExactProvidersAndPreserveOrder(t *testing.T) {
	spec := ContractSpec{ID: "example.com/contracts/value", Major: 1}
	p1, p2 := testDefinition("example.com/contracts/p1"), testDefinition("example.com/contracts/p2")
	p1.Provides, p2.Provides = []ContractSpec{spec}, []ContractSpec{spec}
	required := testDefinition("example.com/contracts/required")
	required.Consumes = []ContractRequirement{{ID: spec.ID, Major: 1, Mode: ContractRequired}}
	many := testDefinition("example.com/contracts/many")
	many.Consumes = []ContractRequirement{{ID: spec.ID, Major: 1, Mode: ContractMany}}
	provide := func(def PluginDefinition, value int) PluginProvider {
		return PluginProvider{Definition: def, New: func() Plugin {
			return pluginFunc(func(r *Registrar) error { return ProvideContract(r, spec, value) })
		}}
	}
	var one, all *ContractRef
	requiredProvider := PluginProvider{Definition: required, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			var err error
			one, err = RequireContract(r, spec, ContractRequired, (*int)(nil))
			return err
		})
	}}
	manyProvider := PluginProvider{Definition: many, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			var err error
			all, err = RequireContract(r, spec, ContractMany, (*int)(nil))
			return err
		})
	}}
	set := testSet(t, provide(p1, 1), provide(p2, 2), requiredProvider, manyProvider)
	for i := range set.Selections {
		switch set.Selections[i].ID {
		case required.ID:
			set.Selections[i].Contracts[0].Providers = []string{p2.ID}
		case many.ID:
			set.Selections[i].Contracts[0].Providers = []string{p2.ID, p1.ID}
		}
	}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	var gotOne int
	if err := one.Call(func(value any) error { gotOne = value.(int); return nil }); err != nil {
		t.Fatal(err)
	}
	if gotOne != 2 {
		t.Fatalf("required bound value=%d, want 2", gotOne)
	}
	var gotAll []int
	if err := all.CallAll(func(values []any) error {
		for _, value := range values {
			gotAll = append(gotAll, value.(int))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotAll, []int{2, 1}) {
		t.Fatalf("many binding order=%v", gotAll)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}

	for name, owners := range map[string][]string{
		"unlinked":   {"example.com/contracts/missing"},
		"duplicate":  {p1.ID, p1.ID},
		"undeclared": {required.ID},
	} {
		t.Run(name, func(t *testing.T) {
			broken := testSet(t, provide(p1, 1), provide(p2, 2), requiredProvider)
			broken.Selections[2].Contracts[0].Providers = owners
			if _, err := InspectPluginPlan(broken); err == nil {
				t.Fatalf("accepted binding %v", owners)
			}
		})
	}
}

func TestLifecycleRollbackReverseStopAndContractLease(t *testing.T) {
	spec := ContractSpec{ID: "example.com/block", Major: 1}
	providerDef := testDefinition("example.com/provider")
	providerDef.Provides = []ContractSpec{spec}
	consumerDef := testDefinition("example.com/consumer")
	consumerDef.Consumes = []ContractRequirement{{ID: spec.ID, Major: 1, Mode: ContractRequired}}
	var ref *ContractRef
	entered, release := make(chan struct{}), make(chan struct{})
	events := []string{}
	var mu sync.Mutex
	provider := PluginProvider{Definition: providerDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			if err := ProvideContract(r, spec, 1); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				mu.Lock()
				events = append(events, "provider-stop")
				mu.Unlock()
				return nil
			}})
		})
	}}
	consumer := PluginProvider{Definition: consumerDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			var err error
			ref, err = RequireContract(r, spec, ContractRequired, (*int)(nil))
			if err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				if err := ref.Call(func(value any) error {
					mu.Lock()
					events = append(events, fmt.Sprintf("consumer-dependency:%d", value.(int)))
					mu.Unlock()
					return nil
				}); err != nil {
					return err
				}
				mu.Lock()
				events = append(events, "consumer-stop")
				mu.Unlock()
				return nil
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider, consumer)); err != nil {
		t.Fatal(err)
	}
	doneCall := make(chan error, 1)
	go func() { doneCall <- ref.Call(func(any) error { close(entered); <-release; return nil }) }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- rt.Close() }()
	for i := 0; i < 1000; i++ {
		if err := ref.Call(func(any) error { return nil }); err != nil {
			break
		}
	}
	select {
	case err := <-closed:
		t.Fatalf("close returned early: %v", err)
	default:
	}
	close(release)
	if err := <-doneCall; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := ref.Call(func(any) error { return nil }); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("after close=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(events, []string{"consumer-dependency:1", "consumer-stop", "provider-stop"}) {
		t.Fatalf("events=%v", events)
	}
}

func TestStartFailureStopsFailingPluginAndRollsBackReverse(t *testing.T) {
	events := []string{}
	mk := func(id string, start func(context.Context) error) PluginProvider {
		def := testDefinition(id)
		return PluginProvider{Definition: def, New: func() Plugin {
			return pluginFunc(func(r *Registrar) error {
				return r.Lifecycle(PluginLifecycle{Start: start, Stop: func(context.Context) error { events = append(events, "stop:"+id); return nil }})
			})
		}}
	}
	a := mk("example.com/start/a", func(context.Context) error { events = append(events, "start:a"); return nil })
	b := mk("example.com/start/b", func(context.Context) error { events = append(events, "start:b"); return errors.New("boom") })
	b.Definition.Requires = []PluginRequirement{{ID: a.Definition.ID, Version: "^1.0.0"}}
	set := testSet(t, a, b)
	if err := NewRuntime().LoadPlugins(context.Background(), set); err == nil {
		t.Fatal("start failure accepted")
	}
	want := []string{"start:a", "start:b", "stop:example.com/start/b", "stop:example.com/start/a"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v want=%v", events, want)
	}
}

func TestStartFailureRevokesCommittedButUnstartedPlugins(t *testing.T) {
	spec := ContractSpec{ID: "example.com/rollback/value", Major: 1}
	failingDef := testDefinition("example.com/rollback/failing")
	failingDef.Provides = []ContractSpec{spec}
	laterDef := testDefinition("example.com/rollback/later")
	laterDef.Consumes = []ContractRequirement{{ID: spec.ID, Major: 1, Mode: ContractRequired}}
	laterDef.Authorities = []AuthorityRequest{{Name: AuthorityHostArgumentsRead, Mode: AuthorityRequired, Reason: "read argv"}}
	var ref *ContractRef
	var args *GuestArgumentsAccess
	var stops []string
	failing := PluginProvider{Definition: failingDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			if err := ProvideContract(r, spec, 7); err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{
				Start: func(context.Context) error { return errors.New("start failed") },
				Stop:  func(context.Context) error { stops = append(stops, "failing"); return nil },
			})
		})
	}}
	later := PluginProvider{Definition: laterDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			var err error
			ref, err = RequireContract(r, spec, ContractRequired, (*int)(nil))
			if err != nil {
				return err
			}
			args, err = r.GuestArguments()
			if err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				stops = append(stops, "later")
				return nil
			}})
		})
	}}
	rt := NewRuntime(WithGuestArguments([]string{"secret"}))
	if err := rt.LoadPlugins(context.Background(), testSet(t, failing, later)); err == nil {
		t.Fatal("accepted failed start")
	}
	if !reflect.DeepEqual(stops, []string{"failing"}) {
		t.Fatalf("stops=%v", stops)
	}
	if err := ref.Call(func(any) error { return nil }); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unstarted contract remained active: %v", err)
	}
	if _, err := args.Args(); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unstarted authority remained active: %v", err)
	}
	if got := rt.Plugins(); len(got) != 0 {
		t.Fatalf("rolled-back plugins=%v", got)
	}
}

func TestPluginPanicsAreAttributedAndCleanupContinues(t *testing.T) {
	def := testDefinition("example.com/panic/plugin")
	tests := []struct {
		name     string
		provider PluginProvider
		phase    PluginPhase
	}{
		{"validate-config", PluginProvider{Definition: def, ValidateConfig: func(json.RawMessage) error { panic("config") }, New: func() Plugin { return pluginFunc(nil) }}, PluginPhaseConfigure},
		{"factory", PluginProvider{Definition: def, New: func() Plugin { panic("factory") }}, PluginPhaseRegister},
		{"register", PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(func(*Registrar) error { panic("register") }) }}, PluginPhaseRegister},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewRuntime().LoadPlugins(context.Background(), testSet(t, tc.provider))
			var pluginErr *PluginError
			if !errors.As(err, &pluginErr) || pluginErr.Plugin != def.ID || pluginErr.Phase != tc.phase || !strings.Contains(err.Error(), "panicked") {
				t.Fatalf("error=%v pluginError=%+v", err, pluginErr)
			}
		})
	}

	startDef := testDefinition("example.com/panic/start")
	accessDef := testDefinition("example.com/panic/unstarted")
	accessDef.Requires = []PluginRequirement{{ID: startDef.ID, Version: "^1.0.0"}}
	accessDef.Authorities = []AuthorityRequest{{Name: AuthorityHostArgumentsRead, Mode: AuthorityRequired, Reason: "argv"}}
	var access *GuestArgumentsAccess
	start := PluginProvider{Definition: startDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			return r.Lifecycle(PluginLifecycle{Start: func(context.Context) error { panic("start") }, Stop: func(context.Context) error { panic("stop") }})
		})
	}}
	unstarted := PluginProvider{Definition: accessDef, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error { var err error; access, err = r.GuestArguments(); return err })
	}}
	err := NewRuntime(WithGuestArguments([]string{"x"})).LoadPlugins(context.Background(), testSet(t, start, unstarted))
	if err == nil || !strings.Contains(err.Error(), "start") || !strings.Contains(err.Error(), "stop") || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("panic lifecycle error=%v", err)
	}
	if _, accessErr := access.Args(); !errors.Is(accessErr, ErrPermissionDenied) {
		t.Fatalf("cleanup stopped after panic: %v", accessErr)
	}
}

func TestValidatePluginSetRunsRegistrationWithoutActivation(t *testing.T) {
	def := testDefinition("example.com/validate/plugin")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostArgumentsRead, Mode: AuthorityRequired, Reason: "argv"}}
	registered, started := 0, 0
	var access *GuestArgumentsAccess
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			registered++
			var err error
			access, err = r.GuestArguments()
			if err != nil {
				return err
			}
			return r.Lifecycle(PluginLifecycle{Start: func(context.Context) error { started++; return nil }})
		})
	}}
	if err := ValidatePluginSet(testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	if registered != 1 || started != 0 {
		t.Fatalf("registered=%d started=%d", registered, started)
	}
	if _, err := access.Args(); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("validation activated access: %v", err)
	}
}

func TestConfigStrictAndRegistrarSealed(t *testing.T) {
	def := testDefinition("example.com/config/plugin")
	def.ConfigSchema = []byte(`{"type":"object","additionalProperties":false}`)
	var handle *ModuleCompileObserver
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			var cfg struct {
				Limit int `json:"limit"`
			}
			if err := r.Config(&cfg); err != nil {
				return err
			}
			var err error
			handle, err = r.ModuleCompileObserver()
			return err
		})
	}}
	def.Authorities = []AuthorityRequest{{Name: AuthorityModuleCompileObserve, Mode: AuthorityRequired, Reason: "observe"}}
	provider.Definition = def
	set := testSet(t, provider)
	set.Selections[0].Config = []byte(`{"limit":1,"unknown":2}`)
	if err := NewRuntime().LoadPlugins(context.Background(), set); err == nil {
		t.Fatal("unknown config accepted")
	}
	set = testSet(t, provider)
	set.Selections[0].Config = []byte(`{"limit":1} {}`)
	if err := NewRuntime().LoadPlugins(context.Background(), set); err == nil {
		t.Fatal("trailing config accepted")
	}
	set = testSet(t, provider)
	set.Selections[0].Config = []byte(`{"limit":1}`)
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if err := handle.Observe(func(ModuleCompiledEvent) {}); err == nil {
		t.Fatal("sealed handle mutated registration")
	}
}

func TestCompilationAndModuleIdentitiesCorrelateWithoutAuthority(t *testing.T) {
	def := testDefinition("example.com/compile/correlation")
	def.Authorities = []AuthorityRequest{
		{Name: AuthorityModuleSourceTransform, Mode: AuthorityRequired, Reason: "read source metadata"},
		{Name: AuthorityModuleCompileObserve, Mode: AuthorityRequired, Reason: "bind source metadata"},
		{Name: AuthorityInstanceInstantiateObserve, Mode: AuthorityRequired, Reason: "bind instance state"},
	}
	var mu sync.Mutex
	pending := map[CompilationIdentity]struct{}{}
	modules := map[ModuleIdentity]struct{}{}
	seenCompilations := map[CompilationIdentity]struct{}{}
	var failures []error
	instances := 0
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			transformer, err := r.ModuleSourceTransformer()
			if err != nil {
				return err
			}
			if err := transformer.Transform(func(ctx ModuleSourceContext, source []byte) ([]byte, error) {
				if ctx.Compilation.IsZero() {
					return nil, errors.New("zero compilation identity")
				}
				mu.Lock()
				pending[ctx.Compilation] = struct{}{}
				seenCompilations[ctx.Compilation] = struct{}{}
				mu.Unlock()
				return nil, nil
			}); err != nil {
				return err
			}
			compiled, err := r.ModuleCompileObserver()
			if err != nil {
				return err
			}
			if err := compiled.Observe(func(event ModuleCompiledEvent) {
				mu.Lock()
				defer mu.Unlock()
				if _, ok := pending[event.Compilation]; !ok {
					t.Error("compiled event did not match source operation")
				}
				if event.Module.Identity().IsZero() {
					t.Error("compiled event has zero module identity")
				}
				delete(pending, event.Compilation)
				modules[event.Module.Identity()] = struct{}{}
			}); err != nil {
				return err
			}
			if err := compiled.OnError(func(event ModuleCompileErrorEvent) {
				mu.Lock()
				delete(pending, event.Compilation)
				failures = append(failures, event.Err)
				mu.Unlock()
			}); err != nil {
				return err
			}
			instantiated, err := r.InstanceInstantiateObserver()
			if err != nil {
				return err
			}
			return instantiated.After(func(event InstantiationEvent) {
				mu.Lock()
				if _, ok := modules[event.Module.Identity()]; !ok {
					t.Error("instance event did not match its compiled module")
				}
				instances++
				mu.Unlock()
			})
		})
	}}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		mod, err := rt.Compile(wasmtest.Module())
		if err != nil {
			t.Fatal(err)
		}
		instance, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rt.Compile([]byte("bad")); err == nil {
		t.Fatal("malformed module compiled")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(pending) != 0 || len(seenCompilations) != 3 || len(modules) != 2 || instances != 2 || len(failures) != 1 || failures[0] == nil {
		t.Fatalf("pending=%d compilations=%d modules=%d instances=%d failures=%v", len(pending), len(seenCompilations), len(modules), instances, failures)
	}
}

func TestModuleCompiledEventCarriesOnlyTheFinalTransformedSourceDigest(t *testing.T) {
	original := wasmtest.Module()
	intermediate := wasmtest.Module(wasmtest.Section(0, append(wasmtest.Name("first"), 1)))
	final := wasmtest.Module(
		wasmtest.Section(0, append(wasmtest.Name("first"), 1)),
		wasmtest.Section(0, append(wasmtest.Name("second"), 2)),
	)

	rt := NewRuntime()
	defer rt.Close()
	rt.hooks.beforeCompile = append(rt.hooks.beforeCompile,
		func(ModuleSourceContext, []byte) ([]byte, error) {
			return append([]byte(nil), intermediate...), nil
		},
		func(_ ModuleSourceContext, source []byte) ([]byte, error) {
			if !reflect.DeepEqual(source, intermediate) {
				t.Fatalf("second transformer source=%x, want intermediate=%x", source, intermediate)
			}
			return append([]byte(nil), final...), nil
		},
	)
	var digests []ModuleSourceDigest
	rt.hooks.afterCompile = append(rt.hooks.afterCompile, func(event ModuleCompiledEvent) {
		digests = append(digests, event.SourceDigest)
	})

	mod, err := rt.Compile(original)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	if len(digests) != 1 || digests[0].IsZero() {
		t.Fatalf("Runtime.Compile digests=%v, want one available digest", digests)
	}
	if want := DigestModuleSource(final); digests[0] != want {
		t.Fatal("compiled event did not identify the final transformed bytes")
	}
	if digests[0] == DigestModuleSource(original) || digests[0] == DigestModuleSource(intermediate) {
		t.Fatal("compiled event identified an earlier source revision")
	}

	compiled, err := Compile(final)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	precompiled, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer precompiled.Close()
	if len(digests) != 2 || !digests[1].IsZero() {
		t.Fatalf("Runtime.Module source digest=%v, want unavailable", digests[1])
	}
}

func TestModuleCloseObserverEmitsOnceWithoutClosingCompiled(t *testing.T) {
	def := testDefinition("example.com/module/lifecycle")
	def.Authorities = []AuthorityRequest{
		{Name: AuthorityModuleCompileObserve, Mode: AuthorityRequired, Reason: "correlate the module"},
		{Name: AuthorityModuleCloseObserve, Mode: AuthorityRequired, Reason: "release module state"},
	}
	var mu sync.Mutex
	var compiledID, closedID ModuleIdentity
	closeCalls := 0
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			compiler, err := r.ModuleCompileObserver()
			if err != nil {
				return err
			}
			if err := compiler.Observe(func(event ModuleCompiledEvent) {
				mu.Lock()
				compiledID = event.Module.Identity()
				mu.Unlock()
			}); err != nil {
				return err
			}
			closer, err := r.ModuleCloseObserver()
			if err != nil {
				return err
			}
			return closer.Observe(func(event ModuleCloseEvent) {
				mu.Lock()
				closedID = event.Module.Identity()
				closeCalls++
				mu.Unlock()
			})
		})
	}}
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	compiled := mod.Compiled()

	const callers = 16
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- mod.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	if compiledID.IsZero() || closedID != compiledID || closeCalls != 1 {
		t.Fatalf("compiled=%v closed=%v close calls=%d", compiledID, closedID, closeCalls)
	}
	mu.Unlock()
	if !moduleView(mod).Identity().IsZero() {
		t.Fatal("closed module wrapper retained its identity")
	}
	if _, err := rt.Instantiate(context.Background(), mod); err == nil || !strings.Contains(err.Error(), "module is closed") {
		t.Fatalf("Runtime.Instantiate after Module.Close error=%v", err)
	}
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatalf("Module.Close closed caller-owned Compiled: %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationMustMatchDeclaredContracts(t *testing.T) {
	def := testDefinition("example.com/contracts/mismatch")
	def.Provides = []ContractSpec{{ID: "example.com/contracts/value", Major: 1}}
	provider := PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(func(*Registrar) error { return nil }) }}
	if err := NewRuntime().LoadPlugins(context.Background(), testSet(t, provider)); err == nil {
		t.Fatal("missing contribution accepted")
	}
}

func TestBoundedManagedAuthorityNarrowGrant(t *testing.T) {
	def := testDefinition("example.com/manage/plugin")
	def.Authorities = []AuthorityRequest{{Name: AuthorityInstanceManage, Mode: AuthorityRequired, Reason: "pool", Scope: AuthorityScope{MaxInstances: 10, MaxMemoryBytes: 1 << 20}}}
	var manager *InstanceManager
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error { var err error; manager, err = r.ManagedInstances(); return err })
	}}
	set := testSet(t, provider)
	set.Selections[0].Grants[0].Scope = AuthorityScope{MaxInstances: 1, MaxMemoryBytes: 64 << 10}
	if err := NewRuntime().LoadPlugins(context.Background(), set); err != nil {
		t.Fatal(err)
	}
	if manager.budget.MaxInstances != 1 || manager.budget.MaxMemoryBytes != 64<<10 {
		t.Fatalf("budget=%+v", manager.budget)
	}
}

func TestManagedOpaqueIdentities(t *testing.T) {
	rt := NewRuntime()
	manager := newPendingInstanceManager("example.com/identity/plugin", AuthorityScope{MaxInstances: 1, MaxMemoryBytes: 64 << 10})
	manager.activate(rt)
	mod, err := rt.Compile(wasmtest.Module())
	if err != nil {
		t.Fatal(err)
	}
	managed, err := manager.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	in := managed.Instance()

	caller := in.beginHostCallScope()
	callerIdentity, err := manager.CallerIdentity(caller)
	if err != nil {
		t.Fatal(err)
	}
	if callerIdentity.IsZero() || callerIdentity != managed.Identity() {
		t.Fatalf("caller=%v managed=%v", callerIdentity, managed.Identity())
	}
	in.pluginState.Load().hostScope.end(caller.generation, caller.parentGeneration)
	if _, err := manager.CallerIdentity(caller); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("expired caller identity=%v", err)
	}
	if err := managed.Close(); err != nil {
		t.Fatal(err)
	}
	if !managed.Identity().IsZero() {
		t.Fatal("closed managed instance retained identity")
	}
}

func TestGuestArgumentsAreRuntimeScopedAndRevoked(t *testing.T) {
	def := testDefinition("example.com/args/plugin")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostArgumentsRead, Mode: AuthorityRequired, Reason: "argv"}}
	var access *GuestArgumentsAccess
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error { var err error; access, err = r.GuestArguments(); return err })
	}}
	rt := NewRuntime(WithGuestArguments([]string{"a", "b"}))
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	args, err := access.Args()
	if err != nil || !reflect.DeepEqual(args, []string{"a", "b"}) {
		t.Fatalf("args=%v err=%v", args, err)
	}
	args[0] = "changed"
	again, _ := access.Args()
	if again[0] != "a" {
		t.Fatal("arguments not defensive")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Args(); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("after close=%v", err)
	}
}

func TestGuestArgumentsEmptyIsActiveAndRevoked(t *testing.T) {
	def := testDefinition("example.com/args/empty")
	def.Authorities = []AuthorityRequest{{Name: AuthorityHostArgumentsRead, Mode: AuthorityRequired, Reason: "argv"}}
	var access *GuestArgumentsAccess
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error { var err error; access, err = r.GuestArguments(); return err })
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	args, err := access.Args()
	if err != nil || args == nil || len(args) != 0 {
		t.Fatalf("empty args=%#v err=%v", args, err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Args(); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("after close=%v", err)
	}
}

func TestObserverViewsAreOpaqueAndCorrelated(t *testing.T) {
	def := testDefinition("example.com/views/plugin")
	def.Authorities = []AuthorityRequest{{Name: AuthorityModuleCompileObserve, Mode: AuthorityRequired, Reason: "module"}, {Name: AuthorityInstanceInstantiateObserve, Mode: AuthorityRequired, Reason: "instance"}, {Name: AuthorityInstanceInvokeIntercept, Mode: AuthorityRequired, Reason: "invoke"}, {Name: AuthorityInstanceInvokeObserve, Mode: AuthorityRequired, Reason: "result"}, {Name: AuthorityInstanceCloseObserve, Mode: AuthorityRequired, Reason: "close"}}
	var seenInstance InstanceIdentity
	var beforeOp OperationIdentity
	events := []string{}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			compile, _ := r.ModuleCompileObserver()
			_ = compile.Observe(func(ModuleCompiledEvent) { events = append(events, "compile") })
			inst, _ := r.InstanceInstantiateObserver()
			_ = inst.After(func(e InstantiationEvent) {
				seenInstance = e.Instance
				events = append(events, "instantiate")
			})
			before, _ := r.InstanceInvokeInterceptor()
			_ = before.Before(func(e InvocationRequest) error {
				beforeOp = e.Operation
				if len(e.Args) > 0 {
					e.Args[0] = ValueI32(99)
				}
				events = append(events, "before")
				return nil
			})
			after, _ := r.InstanceInvokeObserver()
			_ = after.After(func(e InvocationEvent) {
				if e.Operation != beforeOp {
					t.Error("operation identity mismatch")
				}
				if e.Instance != seenInstance {
					t.Error("instance identity mismatch")
				}
				events = append(events, "after")
			})
			closeObserver, _ := r.InstanceCloseObserver()
			_ = closeObserver.After(func(e InstanceCloseEvent) {
				if e.Instance != seenInstance {
					t.Error("close identity mismatch")
				}
				events = append(events, "close")
			})
			return nil
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Compile(wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0x00, 0x00})), wasmtest.Section(3, wasmtest.Vec([]byte{0x00})), wasmtest.Section(7, wasmtest.Vec(append(wasmtest.Name("f"), 0x00, 0x00))), wasmtest.Section(10, wasmtest.Vec([]byte{0x02, 0x00, 0x0b}))))
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Call(context.Background(), "f"); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"compile", "instantiate", "before", "after", "close"}) {
		t.Fatalf("events=%v", events)
	}
}

func TestPostCreateInterceptorRunsBeforeStartAndAbortsTransactionally(t *testing.T) {
	t.Run("before start", func(t *testing.T) {
		def := testDefinition("example.com/instantiate/interceptor")
		def.Authorities = []AuthorityRequest{
			{Name: AuthorityHostImportDefine, Mode: AuthorityRequired, Reason: "start", Scope: AuthorityScope{Modules: []string{"env"}}},
			{Name: AuthorityInstanceInstantiateIntercept, Mode: AuthorityRequired, Reason: "attach state"},
			{Name: AuthorityInstanceInstantiateObserve, Mode: AuthorityRequired, Reason: "observe success"},
		}
		var attached InstanceIdentity
		var events []string
		provider := PluginProvider{Definition: def, New: func() Plugin {
			return pluginFunc(func(r *Registrar) error {
				imports, err := r.HostImports()
				if err != nil {
					return err
				}
				module, err := imports.Module("env")
				if err != nil {
					return err
				}
				module.Func("start", func(HostModule, []uint64, []uint64) {
					if attached.IsZero() {
						t.Error("start ran before post-create attachment")
					}
					events = append(events, "start")
				})
				interceptor, err := r.InstanceInstantiateInterceptor()
				if err != nil {
					return err
				}
				if err := interceptor.After(func(event InstantiationEvent) error {
					attached = event.Instance
					events = append(events, "attach")
					return nil
				}); err != nil {
					return err
				}
				observer, err := r.InstanceInstantiateObserver()
				if err != nil {
					return err
				}
				return observer.After(func(event InstantiationEvent) {
					if event.Instance != attached {
						t.Error("success observer received a different identity")
					}
					events = append(events, "observe")
				})
			})
		}}
		rt := NewRuntime()
		defer rt.Close()
		if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
			t.Fatal(err)
		}
		mod, err := rt.Compile(importedStartModule())
		if err != nil {
			t.Fatal(err)
		}
		instance, err := rt.Instantiate(context.Background(), mod)
		if err != nil {
			t.Fatal(err)
		}
		defer instance.Close()
		if got, want := fmt.Sprint(events), "[attach start observe]"; got != want {
			t.Fatalf("events = %s, want %s", got, want)
		}
	})

	t.Run("failure closes partial instance", func(t *testing.T) {
		attachErr := errors.New("attach failed")
		def := testDefinition("example.com/instantiate/failure")
		def.Authorities = []AuthorityRequest{
			{Name: AuthorityInstanceInstantiateIntercept, Mode: AuthorityRequired, Reason: "attach state"},
			{Name: AuthorityInstanceInstantiateObserve, Mode: AuthorityRequired, Reason: "observe failure"},
			{Name: AuthorityInstanceCloseObserve, Mode: AuthorityRequired, Reason: "discard state"},
		}
		state := map[InstanceIdentity]struct{}{}
		var observed error
		successes := 0
		provider := PluginProvider{Definition: def, New: func() Plugin {
			return pluginFunc(func(r *Registrar) error {
				interceptor, _ := r.InstanceInstantiateInterceptor()
				if err := interceptor.After(func(event InstantiationEvent) error {
					state[event.Instance] = struct{}{}
					return attachErr
				}); err != nil {
					return err
				}
				observer, _ := r.InstanceInstantiateObserver()
				if err := observer.After(func(InstantiationEvent) { successes++ }); err != nil {
					return err
				}
				if err := observer.OnError(func(event InstantiationErrorEvent) { observed = event.Err }); err != nil {
					return err
				}
				closed, _ := r.InstanceCloseObserver()
				return closed.Before(func(event InstanceCloseEvent) { delete(state, event.Instance) })
			})
		}}
		rt := NewRuntime()
		defer rt.Close()
		if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
			t.Fatal(err)
		}
		mod, err := rt.Compile(wasmtest.Module())
		if err != nil {
			t.Fatal(err)
		}
		instance, err := rt.Instantiate(context.Background(), mod)
		if instance != nil || !errors.Is(err, attachErr) {
			t.Fatalf("Instantiate = %p, %v; want nil, attach failure", instance, err)
		}
		if !errors.Is(observed, attachErr) || successes != 0 || len(state) != 0 {
			t.Fatalf("observed=%v successes=%d retained-state=%d", observed, successes, len(state))
		}
	})
}

func TestObserverPanicsAreContainedAndTeardownContinues(t *testing.T) {
	def := testDefinition("example.com/observer/panic")
	def.Authorities = []AuthorityRequest{{Name: AuthorityRuntimeCloseObserve, Mode: AuthorityRequired, Reason: "shutdown"}}
	var events []string
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(r *Registrar) error {
			observer, err := r.RuntimeCloseObserver()
			if err != nil {
				return err
			}
			return observer.Observe(
				func(RuntimeCloseEvent) { events = append(events, "first") },
				func(RuntimeCloseEvent) { events = append(events, "second"); panic("observer") },
			)
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	err := rt.Close()
	if err == nil || !strings.Contains(err.Error(), "RuntimeCloseObserver hook panicked") {
		t.Fatalf("close error=%v", err)
	}
	if !reflect.DeepEqual(events, []string{"second", "first"}) {
		t.Fatalf("observer order/continuation=%v", events)
	}
}

func BenchmarkInspectPluginPlanEmpty(b *testing.B) {
	set := PluginSet{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := InspectPluginPlan(set); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkPluginSet(b *testing.B, n int) PluginSet {
	b.Helper()
	providers := make([]PluginProvider, n)
	for i := 0; i < n; i++ {
		def := testDefinition(fmt.Sprintf("example.com/bench/p%02d", i))
		if i > 0 {
			previous := fmt.Sprintf("example.com/bench/p%02d", i-1)
			contract := ContractSpec{ID: previous + "/contract", Major: 1}
			def.Requires = []PluginRequirement{{ID: previous, Version: "^1.0.0"}}
			def.Consumes = []ContractRequirement{{ID: contract.ID, Major: 1, Mode: ContractRequired}}
			providers[i] = PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(nil) }}
		} else {
			providers[i] = PluginProvider{Definition: def, New: func() Plugin { return pluginFunc(nil) }}
		}
		contract := ContractSpec{ID: def.ID + "/contract", Major: 1}
		providers[i].Definition.Provides = []ContractSpec{contract}
	}
	set := testSet(b, providers...)
	return set
}

func BenchmarkInspectPluginPlanOne(b *testing.B) {
	set := benchmarkPluginSet(b, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := InspectPluginPlan(set); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkInspectPluginPlan32(b *testing.B) {
	set := benchmarkPluginSet(b, 32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := InspectPluginPlan(set); err != nil {
			b.Fatal(err)
		}
	}
}
