package wago

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// Keep this benchmark source portable to beta.10 for an identical harness.
func BenchmarkInvocationGate(b *testing.B) {
	b.Run("ordinary", func(b *testing.B) {
		var gate invocationGate
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			gate.Lock()
			//lint:ignore SA2001 This benchmark measures gate admission and release themselves.
			gate.Unlock()
		}
	})
	b.Run("direct-fast", func(b *testing.B) {
		var gate invocationGate
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if !gate.state.CompareAndSwap(0, invocationGateHeld|invocationGateFast) {
				b.Fatal("acquire")
			}
			gate.Unlock()
		}
	})
	for _, workers := range []int{2, 8, 16, 64} {
		b.Run(fmt.Sprintf("contended/%d", workers), func(b *testing.B) {
			var gate invocationGate
			var wg sync.WaitGroup
			start := make(chan struct{})
			b.ReportAllocs()
			b.ResetTimer()
			for worker := 0; worker < workers; worker++ {
				wg.Add(1)
				go func(worker int) {
					defer wg.Done()
					<-start
					for i := worker; i < b.N; i += workers {
						gate.Lock()
						//lint:ignore SA2001 This benchmark measures contention on an empty critical section.
						gate.Unlock()
					}
				}(worker)
			}
			close(start)
			wg.Wait()
		})
	}
	b.Run("handoff-cancellation", func(b *testing.B) {
		var gate invocationGate
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			gate.Lock()
			base, cancel := context.WithCancel(context.Background())
			ctx := &admissionContext{Context: base, waiting: make(chan struct{})}
			done := make(chan struct{})
			go func() {
				if gate.lockContext(ctx) == nil {
					gate.Unlock()
				}
				close(done)
			}()
			<-ctx.waiting
			cancel()
			gate.Unlock()
			<-done
		}
	})
}

// Exercise the queue through complete calls, alongside the isolated gate cost.
func BenchmarkContendedInvoke(b *testing.B) {
	for _, loop := range []bool{false, true} {
		for _, workers := range []int{2, 8, 16} {
			b.Run(fmt.Sprintf("loop=%t/workers=%d", loop, workers), func(b *testing.B) {
				mod := benchAddOneModule()
				if loop {
					mod = benchBranchHintExecModule(false)
				}
				c := benchMustCompile(b, mod)
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				var wg sync.WaitGroup
				start := make(chan struct{})
				b.ReportAllocs()
				b.ResetTimer()
				for worker := 0; worker < workers; worker++ {
					wg.Add(1)
					go func(worker int) {
						defer wg.Done()
						<-start
						for i := worker; i < b.N; i += workers {
							if _, err := in.Invoke("f", 1000); err != nil {
								b.Error(err)
								return
							}
						}
					}(worker)
				}
				close(start)
				wg.Wait()
			})
		}
	}
}
