//go:build !windows

package wagobench

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wago-org/wago"
	"github.com/wago-org/wasi/p1"
)

func ownedWASISet(tb testing.TB) wago.PluginSet {
	tb.Helper()
	provider := p1.Provider()
	digest, err := wago.DefinitionDigest(provider.Definition)
	if err != nil {
		tb.Fatal(err)
	}
	grants := make([]wago.AuthorityGrant, len(provider.Definition.Authorities))
	for i, request := range provider.Definition.Authorities {
		grants[i] = wago.AuthorityGrant{Name: request.Name, Scope: request.Scope}
	}
	return wago.PluginSet{Providers: []wago.PluginProvider{provider}, Selections: []wago.PluginSelection{{ID: provider.Definition.ID, DefinitionDigest: digest, Direct: true, Grants: grants, Config: json.RawMessage(`{"stdin":"eof","stdout":"discard","stderr":"discard"}`)}}}
}

func BenchmarkWASIOwnedLifecycleDiagnostic(b *testing.B) {
	if !*lifecycleDiagnostics {
		b.Skip("enable with -wago.bench.lifecycle")
	}
	ctx := context.Background()
	b.Run("ProviderSetupClose", func(b *testing.B) {
		set := ownedWASISet(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rt := wago.NewRuntime(wago.WithGuestArguments([]string{"owned-command"}))
			if err := rt.LoadPlugins(ctx, set); err != nil {
				_ = rt.CloseContext(ctx)
				b.Fatal(err)
			}
			if err := rt.CloseContext(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	modules := []corpusModule{minimalWASICommand()}
	for _, m := range commandCorpus(b) {
		if m.ID == "cjson" || m.ID == "tinyxml2" {
			modules = append(modules, m)
		}
	}
	for _, m := range modules {
		b.Run(m.ID, func(b *testing.B) {
			set := ownedWASISet(b)
			rt := wago.NewRuntime(wago.WithGuestArguments(commandArgs(m)))
			if err := rt.LoadPlugins(ctx, set); err != nil {
				_ = rt.CloseContext(ctx)
				b.Fatal(err)
			}
			defer rt.CloseContext(ctx)
			module, err := rt.Compile(m.bytes)
			if err != nil {
				b.Fatal(err)
			}
			defer module.Close()
			run := func() {
				in, err := rt.Instantiate(ctx, module)
				if err != nil {
					b.Fatal(err)
				}
				result, callErr := in.Invoke(m.Command.Export)
				closeErr := in.Close()
				if !commandExitOK(callErr) || closeErr != nil {
					b.Fatalf("command=%v cleanup=%v", callErr, closeErr)
				}
				if err := validateCommandOutput(m, commandOutput{results: result}); err != nil {
					b.Fatal(err)
				}
			}
			run()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				run()
			}
			b.StopTimer()
		})
	}
}
