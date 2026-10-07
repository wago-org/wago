//go:build amd64

package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func main() {
	catalog, err := os.ReadFile("corpus/catalog.json")
	if err != nil {
		panic(err)
	}
	var c struct {
		Benchmarks []struct{ ID, Artifact string }
	}
	if err := json.Unmarshal(catalog, &c); err != nil {
		panic(err)
	}
	wanted := map[string]bool{"json-as": true, "json-as-simd": true, "blake-as": true, "blake-as-simd": true, "nbody": true, "matmul": true, "raytrace": true, "sqlite3-query": true}
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	w.Write([]string{"id", "artifact", "native_bytes", "eligible_store_sites", "spills", "reloads", "shared_functions", "established_functions"})
	for _, entry := range c.Benchmarks {
		if !wanted[entry.ID] {
			continue
		}
		b, err := os.ReadFile(filepath.Join("corpus", entry.Artifact))
		if err != nil {
			panic(err)
		}
		m, err := wasm.DecodeModule(b)
		if err != nil {
			panic(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			panic(err)
		}
		var stats railshot.ModuleStats
		cm, err := railshot.CompileModuleWith(m, railshot.CompileOptions{Stats: &stats})
		if err != nil {
			panic(err)
		}
		hits, spills, reloads, shared, established := 0, 0, 0, 0, 0
		for _, f := range stats.Funcs {
			hits += f.Peephole["store-imm64-signext"]
			spills += f.Spills
			reloads += f.Reloads
			if f.SharedScalar {
				shared++
			} else {
				established++
			}
		}
		values := []int{len(cm.Code), hits, spills, reloads, shared, established}
		row := []string{entry.ID, entry.Artifact}
		for _, v := range values {
			row = append(row, strconv.Itoa(v))
		}
		if err := w.Write(row); err != nil {
			panic(err)
		}
		if err := cm.CodeImage.Close(); err != nil {
			panic(err)
		}
	}
	if err := w.Error(); err != nil {
		panic(fmt.Sprint(err))
	}
}
