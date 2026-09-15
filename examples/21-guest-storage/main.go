// Example 21: callback-scoped guest storage from a plugin host import.
//
// Run:
//
//	go run ./examples/21-guest-storage
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/examples/internal/exampleplugin"
)

//go:embed guest.wasm
var guest []byte

type storagePlugin struct{}

var definition = wago.PluginDefinition{
	ID:          "example.com/wago/storage",
	Name:        "Storage",
	Version:     "1.0.0",
	Description: "Writes through a checked callback-scoped memory view.",
	Stability:   wago.Experimental,
	Provenance: wago.PluginProvenance{
		Repository: "https://example.com/wago/storage",
		License:    "Apache-2.0",
	},
	Authorities: []wago.AuthorityRequest{{
		Name:   wago.AuthorityHostImportDefine,
		Mode:   wago.AuthorityRequired,
		Reason: "define the storage guest API",
		Scope:  wago.AuthorityScope{Modules: []string{"storage"}},
	}},
}

func (storagePlugin) Register(reg *wago.Registrar) error {
	imports, err := reg.HostImports()
	if err != nil {
		return err
	}
	storage, err := imports.Module("storage")
	if err != nil {
		return err
	}
	storage.Func("fill", fill).
		Params(wago.ValI32, wago.ValI32).
		Results(wago.ValI32)
	return nil
}

func fill(caller wago.HostModule, params, results []uint64) {
	module, ok := caller.(wago.GuestStorageHostModule)
	if !ok {
		panic(wago.HostTrap{Err: errors.New("guest storage is unavailable")})
	}
	err := module.WithGuestStorage(func(storage wago.GuestStorage) error {
		info, err := storage.MemoryInfo(0)
		if err != nil {
			return err
		}
		if info.AddressType != wago.GuestMemory32 {
			return errors.New("expected Memory32")
		}
		memory, err := storage.MemoryRange(
			0,
			uint64(uint32(params[0])),
			uint64(uint32(params[1])),
			wago.GuestStorageWrite,
		)
		if err != nil {
			return err
		}
		results[0] = uint64(copy(memory, "Wago"))
		return nil
	})
	if err != nil {
		panic(wago.HostTrap{Err: err})
	}
}

func main() {
	rt := wago.NewRuntime()
	defer rt.Close()
	set := exampleplugin.MustSet(definition, func() wago.Plugin { return storagePlugin{} })
	if err := rt.LoadPlugins(context.Background(), set); err != nil {
		panic(err)
	}
	module, err := rt.Compile(guest)
	if err != nil {
		panic(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module)
	if err != nil {
		panic(err)
	}
	defer instance.Close()
	result, err := instance.Invoke("run")
	if err != nil {
		panic(err)
	}
	fmt.Printf("guest read %q from memory\n", byte(wago.AsI32(result[0])))
}
