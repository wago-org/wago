//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"testing"
)

func TestBoundedCallerCancellationSignal(t *testing.T) {
	for _, mode := range []string{"instance", "prepared", "session"} {
		for _, cancellable := range []bool{false, true} {
			// Session entry has no context parameter; exercise cancellable
			// ambient bindings through the ordinary and prepared entry paths.
			if mode == "session" && cancellable {
				continue
			}
			t.Run(mode+map[bool]string{false: "/background", true: "/cancellable"}[cancellable], func(t *testing.T) {
				ctx := context.Background()
				if cancellable {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					defer cancel()
				}
				imports := NewImports()
				calls := 0
				imports.HostFunc("env", "step", func(c Caller, h HostCall) {
					calls++
					token, ok := resolveHostCaller(c)
					if !ok || !token.valid() || (token.generation&hostCallMayCancel != 0) != cancellable {
						t.Fatal("bounded Caller lost immutable cancellation signal")
					}
					h.SetI32(0, h.I32(0)+1)
				}).Params(ValI32).Results(ValI32)
				compiled, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				in, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				var session *PreparedSession
				if mode == "session" {
					session, err = fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
				}
				restore := bindHostInvocationParent(in, ctx)
				defer restore()
				for i := 0; i < 2; i++ {
					var got []uint64
					switch mode {
					case "instance":
						got, err = in.Invoke("run", 1, 0)
					case "prepared":
						got, err = fn.Invoke(1, 0)
					case "session":
						got, err = session.Invoke2(1, 0)
					}
					if err != nil || len(got) != 1 || got[0] != 1 {
						t.Fatalf("call=%v err=%v", got, err)
					}
				}
				if calls != 2 {
					t.Fatalf("callbacks=%d", calls)
				}
			})
		}
	}
}
