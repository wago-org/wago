//go:build (linux || darwin) && amd64 && !tinygo

package wago

import (
	"fmt"
	"testing"
)

func TestIntegerGlobalSumReadsExportedCellOnEachInvocation(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ := "i32"
		if wide {
			typ = "i64"
		}
		module := watToWasm(t, fmt.Sprintf(`(module
   (global $g (export "g") (mut %[1]s) (%[1]s.const 41))
   (func (export "sum") (param $n %[1]s) (param $a %[1]s) (result %[1]s %[1]s %[1]s %[1]s)
    (local $i %[1]s)
    local.get $a
    loop $loop
     local.get $a global.get $g %[1]s.add local.set $a
     local.get $i %[1]s.const 1 %[1]s.add local.set $i
     local.get $i local.get $n %[1]s.lt_u br_if $loop
    end
    local.get $a local.get $i global.get $g))`, typ))
		for _, regABI := range []bool{false, true} {
			cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithOptimization("reg-abi", regABI).WithOptimization("inline", false)
			c, err := cfg.Compile(module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, code := range []*Compiled{c, publicArtifactRoundTrip(t, c)} {
				in, err := Instantiate(code, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range []uint64{19, ^uint64(0), 0x80000001} {
					if err := in.SetGlobal("g", value); err != nil {
						t.Fatal(err)
					}
					bounds := []uint64{0, 1, 3, 4096, 0xffffffff}
					if wide {
						bounds = append(bounds, 1<<63, ^uint64(0))
					}
					for _, n := range bounds {
						count := n
						if count == 0 {
							count = 1
						}
						seed := uint64(0xfedcba9876543210)
						want := seed + value*count
						v := value
						if !wide {
							count = uint64(uint32(count))
							seed = uint64(uint32(seed))
							want = uint64(uint32(want))
							v = uint64(uint32(v))
						}
						got, err := in.Invoke("sum", n, seed)
						if err != nil || len(got) != 4 || got[0] != seed || got[1] != want || got[2] != count || got[3] != v {
							t.Fatalf("wide=%t abi=%t value=%x n=%x: %v %v, want[%x %x %x %x]", wide, regABI, value, n, got, err, seed, want, count, v)
						}
					}
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
