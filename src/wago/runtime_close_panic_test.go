package wago

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRuntimeCloseSanitizesShutdownPanicsAndContinues(t *testing.T) {
	secret := strings.Repeat("secret-token-", 1024)
	var events []string
	def := testDefinition("example.com/close/panic")
	def.Authorities = []AuthorityRequest{{Name: AuthorityRuntimeCloseObserve, Mode: AuthorityRequired, Reason: "panic containment"}}
	provider := PluginProvider{Definition: def, New: func() Plugin {
		return pluginFunc(func(reg *Registrar) error {
			observer, err := reg.RuntimeCloseObserver()
			if err != nil {
				return err
			}
			if err := observer.Observe(
				func(RuntimeCloseEvent) { events = append(events, "continues") },
				func(RuntimeCloseEvent) { events = append(events, "panics"); panic(secret) },
			); err != nil {
				return err
			}
			return reg.Lifecycle(PluginLifecycle{Stop: func(context.Context) error {
				events = append(events, "stop")
				panic(secret)
			}})
		})
	}}
	rt := NewRuntime()
	if err := rt.LoadPlugins(context.Background(), testSet(t, provider)); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	err := rt.WaitClosed(context.Background())
	if !errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("WaitClosed error = %v, want ErrCallbackPanic", err)
	}
	firstResult := err.Error()
	if strings.Contains(firstResult, secret) {
		t.Fatal("shutdown error retained recovered panic value")
	}
	if len(firstResult) > 512 {
		t.Fatalf("shutdown panic report is unbounded: %d bytes", len(firstResult))
	}
	want := "[panics continues stop]"
	if got := strings.Join(events, " "); "["+got+"]" != want {
		t.Fatalf("shutdown events = %v, want %s", events, want)
	}
	if err := rt.WaitClosed(context.Background()); err == nil || err.Error() != firstResult {
		t.Fatalf("second WaitClosed = %v, want %q", err, firstResult)
	}
}
