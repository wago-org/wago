//go:build windows && amd64 && !tinygo

package wago

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWindowsTypedI64LiveRouteDisableSwitch(t *testing.T) {
	const childEnv = "WAGO_TEST_WINDOWS_I64_LIVE_DISABLED_CHILD"
	if os.Getenv(childEnv) != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsTypedI64LiveRouteDisableSwitch$")
		cmd.Env = append(os.Environ(), "WAGO_PRIVATE_NUMERIC_LIVE_ROUTE=0", childEnv+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("disabled live-route child: %v\n%s", err, output)
		}
		return
	}
	if privateNumericLiveRouteEnabled {
		t.Fatal("disabled live-route switch was ignored during initialization")
	}
	c, err := Compile(goHostSegmentConfig().WithBoundsChecks(BoundsChecksExplicit), cancelBoundedTypedI64Single)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int64) int64 { return v + 1 }).Params(ValI64).Results(ValI64)
	in, err := Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.eng.PreparedScalarHost() != nil {
		t.Fatal("disabled live-route switch still prepared the Windows bridge")
	}
	ic, err := in.fillInvokeCache("run")
	if err != nil {
		t.Fatal(err)
	}
	if in.contextBoundedTypedI64Eligible(ic) {
		t.Fatal("disabled live-route switch still selected the Windows bridge")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if got, err := in.InvokeContext(ctx, "run", 41); err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("disabled live-route parked fallback: %v, %v", got, err)
	}
}

func TestWindowsTypedI64LiveAsyncCancellationAndClose(t *testing.T) {
	if codeProfileEnabled || !detachedNumericHostEnabled || !privateNumericLiveRouteEnabled || !integerNumericHostEnabled {
		t.Skip("private integer host bridge disabled")
	}
	for _, name := range []string{"cancel", "deadline", "close"} {
		t.Run(name, func(t *testing.T) {
			c, err := Compile(goHostSegmentConfig().WithBoundsChecks(BoundsChecksExplicit), cancelBoundedTypedI64Single)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			started, release := make(chan struct{}), make(chan struct{})
			imports := NewImports()
			imports.HostFunc("env", "step", func(v int64) int64 {
				close(started)
				<-release
				return v + 1
			}).Params(ValI64).Results(ValI64)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			ic, err := in.fillInvokeCache("run")
			if err != nil || !in.contextBoundedTypedI64Eligible(ic) {
				t.Fatalf("Windows narrow route unavailable: %v", err)
			}
			in.syncHosts[0].fn = nil // generic fallback cannot call this import
			var ctx context.Context
			var cancel context.CancelFunc
			if name == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			callDone := make(chan error, 1)
			go func() {
				_, err := in.InvokeContext(ctx, "run", 41)
				callDone <- err
			}()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("host callback did not start")
			}
			if name == "close" {
				closeDone := make(chan error, 1)
				go func() { closeDone <- in.Close() }()
				select {
				case err := <-closeDone:
					if err != nil {
						t.Fatalf("logical Close: %v", err)
					}
				case <-time.After(10 * time.Second):
					t.Fatal("logical Close did not return while callback was parked")
				}
				close(release)
				select {
				case <-callDone:
				case <-time.After(10 * time.Second):
					t.Fatal("active invocation did not unwind after Close")
				}
				if err := in.waitTerminalClose(); err != nil {
					t.Fatalf("terminal Close: %v", err)
				}
				return
			}
			if name == "cancel" {
				cancel()
			} else {
				select {
				case <-ctx.Done():
				case <-time.After(10 * time.Second):
					t.Fatal("context deadline did not expire while callback was parked")
				}
			}
			close(release)
			select {
			case err := <-callDone:
				want := context.Canceled
				if name == "deadline" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) {
					t.Fatalf("asynchronous cancellation: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("cancelled invocation did not finish")
			}
		})
	}
}
