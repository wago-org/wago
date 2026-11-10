//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestTailDynamicPublishedRV64FactoryCycle(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared%t", shared), func(t *testing.T) {
			x := tailDynamicProxyFixtureForTest(t, shared, true)
			for _, d := range x.dispatchers {
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			converted := make(map[*coreruntime.CodeBuffer]tailRAFunction)
			var owners []*tailPublishedOwnerForTest
			guest := len(x.root.Bytes())
			for _, owner := range []*tailRADynamicOwner{&x.oa, &x.ob} {
				for _, functions := range [][]tailRAFunction{owner.functions, owner.targets} {
					for i, f := range functions {
						published, ok := converted[f.code]
						if !ok {
							var view *tailPublishedOwnerForTest
							published, view = tailPublishedFunctionForTest(t, f)
							converted[f.code] = published
							owners = append(owners, view)
							guest += len(f.snapshot)
						}
						f.published, f.code = published.published, nil
						functions[i] = f
					}
				}
			}
			x.a, x.b = x.oa.functions[0], x.ob.functions[0]
			for _, f := range []tailRAFunction{x.a, x.b} {
				s := tailDynamicSiteForTest(t, f, []tailRADynamicOwner{x.oa, x.ob}, f.info.FrameBytes-8)
				s.Continuations = append(s.Continuations, NativeTailCodeRange{x.root.Base(), uintptr(len(x.root.Bytes()))})
				// A revoked borrowed view must fail before any capability is bound.
				owners[0].err = ErrUnsupported
				if d, err := NewNativeTailDispatcher([]NativeTailSite{s}, guest); d != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("revoked view reached factory publication", err)
				}
				owners[0].err = nil
				x.dispatchers = append(x.dispatchers, tailDynamicBindForTest(t, s, guest))
			}
			if err := x.invoke(t, x.root.Base(), 2049); err != nil || !x.resultsMatch() || binary.LittleEndian.Uint64(x.f.witness[8:]) != 0 || binary.LittleEndian.Uint32(x.f.witness[240:]) != 0 {
				t.Fatal("borrowed-image proxy cycle/constant stack", err)
			}
			x.f.assertReturned(t)
			if n := testing.AllocsPerRun(4, func() {
				for _, f := range converted {
					if err := f.published.Validate(); err != nil {
						panic(err)
					}
				}
				if err := x.invoke(t, x.root.Base(), 2049); err != nil {
					panic(err)
				}
			}); n != 0 {
				t.Fatal("borrowed-image warm execution allocations", n)
			}
			for _, d := range x.dispatchers {
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			for buffer, f := range converted {
				if buffer.Base() != f.published.Base() || len(buffer.Bytes()) != len(f.published.Bytes()) || f.published.Validate() != nil {
					t.Fatal("dispatcher close retired borrowed image")
				}
			}
		})
	}
}
