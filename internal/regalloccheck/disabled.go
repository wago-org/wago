//go:build !wago_regalloccheck

package regalloccheck

// Ordinary builds retain API-compatible stubs; guarded callers must not prepare
// or retain checker state. Layout and binary-symbol tests enforce that boundary.
const Enabled = false

type State struct{}
type cell struct{}

func (*State) Fresh(int) Value                     { return nil }
func (*State) Put(Location, Value)                 {}
func (*State) Read(Location, int) Value            { return nil }
func (*State) Seed(Location, int) Value            { return nil }
func (*State) Apply(Effect)                        {}
func (*State) Expect(string, Location, Value)      {}
func (*State) Meet(*State)                         {}
func (*State) Clone() *State                       { return nil }
func (*State) ExpectKnown(string, Location, Value) {}
