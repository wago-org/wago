//go:build ignore

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	wago "github.com/wago-org/wago"
)

type inventoryWorkload struct{ ID, Artifact, ABI string }

func main() {
	appRoot := "/Users/work/Code/Web/wasm.fyi/corpora/applications"
	coreRoot := "/Users/work/Code/Web/wasm.fyi/.wasmbench/corpus-cache"
	for _, kind := range []string{"app", "core"} {
		root := appRoot
		if kind == "core" {
			root = coreRoot
		}
		data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
		mustInventory(err)
		var workloads []inventoryWorkload
		if kind == "app" {
			mustInventory(json.Unmarshal(data, &workloads))
		} else {
			var manifest struct{ Workloads []inventoryWorkload }
			mustInventory(json.Unmarshal(data, &manifest))
			workloads = manifest.Workloads
		}
		out := filepath.Join(os.Args[1], kind)
		mustInventory(os.MkdirAll(out, 0755))
		count := 0
		for _, w := range workloads {
			if kind == "core" && (w.ABI != "core" || !strings.HasPrefix(w.ID, "wago/") && !strings.HasPrefix(w.ID, "mechanisms/")) {
				continue
			}
			code, err := os.ReadFile(filepath.Join(root, w.Artifact))
			mustInventory(err)
			compiled, err := wago.CompileWithConfig(wago.NewRuntimeConfig().WithOptimization("literal-memory-index", os.Args[2] == "1"), code)
			mustInventory(err)
			name := filepath.Base(w.ID)
			if kind == "core" {
				name = strings.ReplaceAll(w.ID, "/", "__")
			}
			f, err := os.Create(filepath.Join(out, name+".bin"))
			mustInventory(err)
			_, err = compiled.WriteCodeTo(f)
			mustInventory(err)
			mustInventory(f.Close())
			mustInventory(compiled.Close())
			count++
		}
		if count != (map[string]int{"app": 102, "core": 46}[kind]) {
			panic("incomplete inventory")
		}
	}
}

func mustInventory(err error) {
	if err != nil {
		panic(err)
	}
}
