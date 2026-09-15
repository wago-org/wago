package wago

import "testing"

func BenchmarkCurrentInvocationReservation(b *testing.B) {
	for _, mode := range []string{"none", "inline", "fallback", "inline-with-fallback", "nested"} {
		b.Run(mode, func(b *testing.B) {
			var in Instance
			state := in.ensurePluginState()
			state.invocationID = 1
			first, second := &pluginOperationReservation{}, &pluginOperationReservation{}
			var want *pluginOperationReservation
			if mode != "none" {
				in.swapInvocationReservation(first)
				want = first
			}
			if mode == "fallback" || mode == "inline-with-fallback" {
				state.invocationID = 2
				in.swapInvocationReservation(second)
				if mode == "inline-with-fallback" {
					state.invocationID = 1
				} else {
					want = second
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			if mode == "nested" {
				for i := 0; i < b.N; i++ {
					previous := in.swapInvocationReservation(second)
					got := currentInvocationReservation(&in)
					in.swapInvocationReservation(previous)
					if got != second || previous != first {
						b.Fatal("nested reservation lost")
					}
				}
			} else {
				for i := 0; i < b.N; i++ {
					if got := currentInvocationReservation(&in); got != want {
						b.Fatal("reservation lost")
					}
				}
			}
		})
	}
}
