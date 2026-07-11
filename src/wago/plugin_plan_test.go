package wago

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type planTestExtension struct {
	name     string
	requires []string
	before   []string
	after    []string
	register func(*Registry) error
}

func TestRegistryConfigAndHostEnvironmentAreCapabilityScoped(t *testing.T) {
	reg := &Registry{grants: map[PluginCapability]struct{}{PluginHostEnvironment: {}}, config: json.RawMessage(`{"limit":7}`)}
	var cfg struct {
		Limit int `json:"limit"`
	}
	if err := reg.Config(&cfg); err != nil || cfg.Limit != 7 {
		t.Fatalf("Config = %+v, %v", cfg, err)
	}
	if _, err := reg.HostEnvironment(); err != nil {
		t.Fatalf("HostEnvironment: %v", err)
	}
	if _, err := (&Registry{grants: map[PluginCapability]struct{}{}}).HostEnvironment(); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ungranted HostEnvironment = %v", err)
	}
}

func (e *planTestExtension) Info() ExtensionInfo {
	return ExtensionInfo{
		ID: e.name, Name: e.name, Version: "1.0.0", Repository: "https://example.com/" + e.name,
		License: "Apache-2.0", Requires: e.requires, Before: e.before, After: e.after,
	}
}

func (e *planTestExtension) Register(reg *Registry) error {
	if e.register != nil {
		return e.register(reg)
	}
	return nil
}

func registerPlanTestPlugin(t *testing.T, name string, factory ExtensionFactory) {
	t.Helper()
	RegisterExtension(name, factory)
}

func TestLoadPluginsResolvesDependenciesAndStableOrder(t *testing.T) {
	var registered []string
	makeFactory := func(name string, requires, before, after []string) ExtensionFactory {
		return func() Extension {
			return &planTestExtension{name: name, requires: requires, before: before, after: after, register: func(*Registry) error {
				registered = append(registered, name)
				return nil
			}}
		}
	}
	registerPlanTestPlugin(t, "plan-order-a", makeFactory("plan-order-a", nil, nil, nil))
	registerPlanTestPlugin(t, "plan-order-b", makeFactory("plan-order-b", []string{"plan-order-a"}, nil, nil))
	registerPlanTestPlugin(t, "plan-order-c", makeFactory("plan-order-c", nil, []string{"plan-order-b"}, nil))

	rt := NewRuntime()
	err := rt.LoadPlugins([]PluginConfig{{Name: "plan-order-b"}, {Name: "plan-order-a"}, {Name: "plan-order-c"}})
	if err != nil {
		t.Fatalf("LoadPlugins: %v", err)
	}
	if want := []string{"plan-order-a", "plan-order-c", "plan-order-b"}; !reflect.DeepEqual(registered, want) {
		t.Fatalf("registration order = %v, want %v", registered, want)
	}
}

func TestLoadPluginsRejectsUngrantAndCommitsNothing(t *testing.T) {
	registerPlanTestPlugin(t, "plan-denied", func() Extension {
		return &planTestExtension{name: "plan-denied", register: func(reg *Registry) error {
			reg.ImportModule("denied").Func("f", func(HostModule, []uint64, []uint64) {})
			return nil
		}}
	})
	rt := NewRuntime()
	err := rt.LoadPlugins([]PluginConfig{{Name: "plan-denied"}})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("LoadPlugins error = %v, want ErrPermissionDenied", err)
	}
	if got := rt.Extensions(); len(got) != 0 {
		t.Fatalf("failed plan committed extensions: %v", got)
	}
	if len(rt.HostImports()) != 0 {
		t.Fatal("failed plan committed imports")
	}
}

func TestLoadPluginsRejectsCycle(t *testing.T) {
	registerPlanTestPlugin(t, "plan-cycle-a", func() Extension {
		return &planTestExtension{name: "plan-cycle-a", after: []string{"plan-cycle-b"}}
	})
	registerPlanTestPlugin(t, "plan-cycle-b", func() Extension {
		return &planTestExtension{name: "plan-cycle-b", after: []string{"plan-cycle-a"}}
	})
	err := NewRuntime().LoadPlugins([]PluginConfig{{Name: "plan-cycle-a"}, {Name: "plan-cycle-b"}})
	if err == nil {
		t.Fatal("LoadPlugins accepted a cycle")
	}
}
