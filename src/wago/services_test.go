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
