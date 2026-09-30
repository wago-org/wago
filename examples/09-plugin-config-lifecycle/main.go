// Example 09: plugin configuration, guest arguments, and lifecycle.
//
// Run:
//
//	go run ./examples/09-plugin-config-lifecycle
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
)

type config struct {
	Prefix string `json:"prefix"`
}

type reporter struct {
	config config
	args   *wago.GuestArgumentsAccess
}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/reporter",
	Name:        "Reporter",
	Version:     "1.0.0",
	Description: "Prints reviewed configuration and guest arguments.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/reporter",
		License:    "Apache-2.0",
	},
	ConfigSchema: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["prefix"],
  "properties": {"prefix": {"type": "string", "minLength": 1}}
}`),
	Authorities: []wago.AuthorityRequest{{
		Name:   wago.AuthorityHostArgumentsRead,
		Mode:   wago.AuthorityOptional,
		Reason: "label the arguments passed to the guest",
	}},
}

func (p *reporter) Register(reg *wago.Registrar) error {
	if err := reg.Config(&p.config); err != nil {
		return err
	}
	if reg.Granted(wago.AuthorityHostArgumentsRead) {
		args, err := reg.GuestArguments()
		if err != nil {
			return err
		}
		p.args = args
	}

	return reg.Lifecycle(wago.PluginLifecycle{
		Start: func(context.Context) error {
			var args []string
			if p.args != nil {
				var err error
				args, err = p.args.Args()
				if err != nil {
					return err
				}
			}
			fmt.Printf("%s started with args %q\n", p.config.Prefix, args)
			return nil
		},
		Stop: func(context.Context) error {
			fmt.Printf("%s stopped\n", p.config.Prefix)
			return nil
		},
	})
}

func main() {
	provider := wago.PluginProvider{
		Definition: definition,
		New:        func() wago.Plugin { return &reporter{} },
		ValidateConfig: func(raw json.RawMessage) error {
			var value config
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			if value.Prefix == "reserved" {
				return errors.New("prefix is reserved")
			}
			return nil
		},
	}
	set := exampleplugin.MustSetProvider(provider, json.RawMessage(`{"prefix":"demo"}`))

	rt := wago.NewRuntime(wago.WithGuestArguments([]string{"input.wasm", "--verbose"}))
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}
	if err := rt.CloseContext(context.Background()); err != nil {
		panic(err)
	}
}
