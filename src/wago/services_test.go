package wago

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestServiceRequirementOrdersAndBindsPlugins(t *testing.T) {
	providerReg := &Registry{hooks: &HookRegistry{}}
	if err := ProvideService(providerReg, "test.counter/v1", 42); err != nil {
		t.Fatal(err)
	}
	consumerReg := &Registry{hooks: &HookRegistry{}}
	ref, err := RequireService(consumerReg, "test.counter/v1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ref.Get(); err == nil {
		t.Fatal("service was readable during registration")
	}
	plan, err := resolveServiceOrder([]plannedExtension{
		{name: "consumer", info: ExtensionInfo{ID: "consumer"}, reg: consumerReg},
		{name: "provider", info: ExtensionInfo{ID: "provider"}, reg: providerReg},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{plan[0].name, plan[1].name}; !reflect.DeepEqual(got, []string{"provider", "consumer"}) {
		t.Fatalf("service order = %v", got)
	}
	value, err := ref.Get()
	if err != nil || value.(int) != 42 {
		t.Fatalf("bound service = %v, %v", value, err)
	}
}

type programmaticServiceExtension struct {
	id      string
	provide any
	ref     **ServiceRef
}

func (e *programmaticServiceExtension) Info() ExtensionInfo { return ExtensionInfo{ID: e.id} }
func (e *programmaticServiceExtension) Register(reg *Registry) error {
	if e.provide != nil {
		return ProvideService(reg, "test.programmatic/v1", e.provide)
	}
	ref, err := RequireService(reg, "test.programmatic/v1", (*int)(nil))
	if err == nil && e.ref != nil {
		*e.ref = ref
	}
	return err
}

func TestProgrammaticUseResolvesServicesTransactionally(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	var ref *ServiceRef
	consumer := &programmaticServiceExtension{id: "consumer", ref: &ref}
	if err := rt.Use(consumer); err == nil {
		t.Fatal("consumer loaded without its service provider")
	}
	if _, ok := rt.Extension("consumer"); ok {
		t.Fatal("failed consumer registration mutated the runtime")
	}
	if err := rt.Use(&programmaticServiceExtension{id: "provider", provide: 42}); err != nil {
		t.Fatalf("Use provider: %v", err)
	}
	if err := rt.Use(consumer); err != nil {
		t.Fatalf("Use consumer: %v", err)
	}
	value, err := ref.Get()
	if err != nil || value != 42 {
		t.Fatalf("bound service = %v, %v", value, err)
	}
	if err := rt.Use(&programmaticServiceExtension{id: "duplicate", provide: 7}); !errors.Is(err, ErrExtensionConflict) {
		t.Fatalf("duplicate provider error = %v, want conflict", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := ref.Get(); err == nil {
		t.Fatal("service reference remained active after runtime close")
	}
}

func TestProgrammaticUseRejectsServiceTypeBeforeCommit(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	if err := rt.Use(&programmaticServiceExtension{id: "provider", provide: "wrong"}); err != nil {
		t.Fatalf("Use provider: %v", err)
	}
	var ref *ServiceRef
	consumer := &programmaticServiceExtension{id: "consumer", ref: &ref}
	if err := rt.Use(consumer); err == nil {
		t.Fatal("consumer accepted an incompatible service provider")
	}
	if _, ok := rt.Extension("consumer"); ok {
		t.Fatal("type mismatch mutated the runtime")
	}
	if ref != nil {
		if _, err := ref.Get(); err == nil {
			t.Fatal("rejected service reference became active")
		}
	}
}

type lifecycleTestExtension struct {
	name      string
	events    *[]string
	startFail bool
	stopFail  bool
}

func (e *lifecycleTestExtension) Info() ExtensionInfo    { return ExtensionInfo{ID: e.name} }
func (*lifecycleTestExtension) Register(*Registry) error { return nil }
func (e *lifecycleTestExtension) Start(context.Context, *PluginHost) error {
	*e.events = append(*e.events, "start:"+e.name)
	if e.startFail {
		return errors.New("start failed")
	}
	return nil
}
func (e *lifecycleTestExtension) Stop(context.Context) error {
	*e.events = append(*e.events, "stop:"+e.name)
	if e.stopFail {
		return errors.New("stop failed")
	}
	return nil
}

func TestPluginLifecycleStartsAndStopsInReverse(t *testing.T) {
	var events []string
	a := &lifecycleTestExtension{name: "a", events: &events}
	b := &lifecycleTestExtension{name: "b", events: &events}
	plan := []plannedExtension{
		{name: "a", ext: a, info: a.Info(), reg: &Registry{hooks: &HookRegistry{}}},
		{name: "b", ext: b, info: b.Info(), reg: &Registry{hooks: &HookRegistry{}}},
	}
	rt := NewRuntime()
	if err := rt.commitPluginPlan(plan); err != nil {
		t.Fatal(err)
	}
	if err := rt.startPluginPlan(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"start:a", "start:b", "stop:b", "stop:a"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("lifecycle = %v, want %v", events, want)
	}
}
