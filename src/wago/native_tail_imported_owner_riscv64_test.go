//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

func TestRV64PrivateTailImportedOwnerGeneration(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				in, producer, _, _ := rv64ImportCertificateFixture(t, vector, artifact)
				var scope rv64NativeTableTargets
				if err := rv64TableTargetRefresh(&scope, in); err != nil {
					t.Fatal(err)
				}
				defer scope.release()
				plans := rv64GenerationPlanForTest(t, in, vector)
				plans[0].FunctionIndex = uint32(in.c.NumImports)
				var g *rv64NativeTailGeneration
				create := func(limits rv64NativeTailGenerationLimits) error {
					return rv64GenerationLockedForTest(func() error {
						var err error
						g, err = rv64NewDefinedTailGeneration(in, &scope, plans, limits)
						return err
					})
				}
				defer func() {
					if err := rv64GenerationLockedForTest(func() error { return g.Close() }); err != nil {
						t.Error(err)
					}
				}()
				if err := create(rv64NativeTailGenerationLimits{railshot.MaxCodeBytes, railshot.MaxModuleBytes}); err != nil {
					t.Fatal("construct imported snapshot generation", err)
				}
				if len(g.owners) != 2 || g.callerOwner == nil || len(g.snapshots) != 2 || g.snapshots[1] != nil {
					t.Fatal("physical roots conflated with canonical descriptor owners")
				}
				exact := rv64NativeTailGenerationLimits{g.codeBytes, g.metadataBytes}
				if g.codeBytes <= len(in.c.code)+len(producer.c.code)+in.pluginState.Load().railshotInvocation.nativeImports.codeBytes {
					t.Fatal("guest and wrapper union omitted")
				}
				if err := rv64GenerationLockedForTest(g.Close); err != nil {
					t.Fatal(err)
				}
				for _, limits := range []rv64NativeTailGenerationLimits{{exact.code - 1, exact.metadata}, {exact.code, exact.metadata - 1}} {
					if err := create(limits); !errors.Is(err, railshot.ErrUnsupported) || g != nil || scope.tailBudget.count != 0 {
						t.Fatal("below complete charge admitted", err)
					}
				}
				record := &in.pluginState.Load().railshotInvocation.nativeImports.records[0]
				savedCertificate := record.certificate
				record.certificate = nil
				err := create(exact)
				record.certificate = savedCertificate
				if !errors.Is(err, railshot.ErrUnsupported) || g == nil || !g.reserved {
					t.Fatal("failed certified snapshot lost partial ownership", err)
				}
				if err := rv64GenerationLockedForTest(g.Close); err != nil || scope.tailBudget.count != 0 {
					t.Fatal("failed certified snapshot retained quota", err)
				}
				if err := create(exact); err != nil {
					t.Fatal("exact imported charge", err)
				}
				activate := func() error {
					return rv64GenerationLockedForTest(func() error { return rv64ActivateNativeTailGenerations([]*rv64NativeTailGeneration{g}) })
				}
				if err := activate(); err != nil {
					t.Fatal(err)
				}
				if n := testing.AllocsPerRun(4, func() {
					if err := activate(); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm imported generation allocated", n)
				}
				if err := producer.Close(); err != nil || producer.resourcesClosed || activate() != nil {
					t.Fatal("logical producer close invalidated retained generation", err)
				}
			})
		}
	}
}
