//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"fmt"
	"slices"
	"testing"
)

func TestRV64GCMemoryFeatureMetadataBinding(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, maximum := range []uint32{0, 257} {
			t.Run(fmt.Sprintf("wide%t-max%d", wide, maximum), func(t *testing.T) {
				c, err := NewRuntimeConfig().Compile(gcMemoryCombinationSource(wide, false, maximum))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
						base := c
						if artifact {
							blob, err := c.MarshalBinary()
							if err != nil {
								t.Fatal(err)
							}
							base, err = LoadTrustedArtifact(blob)
							if err != nil {
								t.Fatal(err)
							}
							defer base.Close()
						}
						view := base.executionView()
						if !rv64ArtifactContractValid(view) || view.validate() != nil {
							t.Fatal("valid GC memory image lost its execution binding")
						}
						for name, mutate := range map[string]func(*Compiled){
							"address-width":   func(v *Compiled) { v.memoryDir.defs[0].Addr64 = !v.memoryDir.defs[0].Addr64 },
							"has-maximum":     func(v *Compiled) { v.memoryDir.defs[0].HasMax = !v.memoryDir.defs[0].HasMax },
							"maximum":         func(v *Compiled) { v.memoryDir.defs[0].Max++ },
							"minimum":         func(v *Compiled) { v.memoryDir.defs[0].Min++ },
							"shared":          func(v *Compiled) { v.memoryDir.defs[0].Shared = true },
							"reservation":     func(v *Compiled) { v.MemMaxPages++ },
							"missing-binding": func(v *Compiled) { v.setRV64ExecutionContract(nil) },
						} {
							t.Run(name, func(t *testing.T) {
								candidate := rv64ReferenceStartCopy(view)
								directory := *candidate.memoryDir
								directory.defs = slices.Clone(directory.defs)
								candidate.memoryDir = &directory
								mutate(candidate)
								if err := candidate.validate(); err == nil {
									t.Fatal("changed GC memory metadata was admitted")
								}
								rv64RequireRejectedArtifact(t, candidate)
							})
						}
					})
				}
			})
		}
	}
}
