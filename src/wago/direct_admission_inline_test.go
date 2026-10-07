package wago

import "testing"

func TestDirectAdmissionFastLease(t *testing.T) {
	for _, tc := range []struct {
		name     string
		state    uint32
		managed  bool
		borrowed bool
		want     bool
	}{
		{name: "private", want: true},
		{name: "busy", state: 1},
		{name: "closed", state: instanceInvocationClosed},
		{name: "closing busy", state: instanceInvocationClosed | 1},
		{name: "managed", managed: true},
		{name: "borrowed", borrowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &Instance{}
			in.invocationState.Store(tc.state)
			if tc.managed {
				in.rt = &Runtime{}
			}
			if tc.borrowed {
				state := &instancePluginState{}
				state.guestStorageBorrow.Store(1)
				in.pluginState.Store(state)
			}
			if got := in.tryBeginDirectInvocation(); got != tc.want {
				t.Fatalf("admission = %v, want %v", got, tc.want)
			}
			wantState := tc.state
			if tc.want {
				wantState = 1
			}
			if got := in.invocationState.Load(); got != wantState {
				t.Fatalf("lease state = %d, want %d", got, wantState)
			}
			if tc.want {
				in.endDirectInvocation()
				if in.invocationState.Load() != 0 {
					t.Fatal("lease was not released")
				}
			}
		})
	}
}

func TestDirectAdmissionGateOwnership(t *testing.T) {
	for _, tc := range []struct {
		name        string
		state       uint32
		flags       uint32
		nilGate     bool
		held, valid bool
	}{
		{name: "valid", held: true, valid: true},
		{name: "absent", nilGate: true},
		{name: "busy", state: invocationGateHeld},
		{name: "revoked", state: invocationGateRevoked},
		{name: "shared", flags: executionFlagNativeControlShared, held: true},
		{name: "imported GC", flags: executionFlagImportedGCDomain, held: true},
		{name: "dynamic GC", flags: executionFlagDynamicGCDomain, held: true},
		{name: "store GC", flags: executionFlagStoreOwnedGCCollector, held: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &Instance{}
			in.executionFlags.Store(tc.flags)
			var gate invocationGate
			gate.state.Store(tc.state)
			fn := &WasmFunc{in: in, directGate: &gate}
			if tc.nilGate {
				fn.directGate = nil
			}
			held, valid := fn.tryDirectGateState()
			if held != tc.held || valid != tc.valid {
				t.Fatalf("gate = (%v, %v), want (%v, %v)", held, valid, tc.held, tc.valid)
			}
			want := tc.state
			if held {
				want = invocationGateHeld | invocationGateFast
			}
			if got := gate.state.Load(); got != want {
				t.Fatalf("gate state = %d, want %d", got, want)
			}
			if held {
				gate.Unlock()
			}
			// The original combined helper retains responsibility for releasing
			// an acquired gate whose fast state failed validation.
			if got := fn.tryDirectGate(); got != tc.valid {
				t.Fatalf("combined admission = %v, want %v", got, tc.valid)
			}
			if tc.valid {
				gate.Unlock()
			}
			if got := gate.state.Load(); got != tc.state {
				t.Fatalf("final gate state = %d, want %d", got, tc.state)
			}
		})
	}
}
