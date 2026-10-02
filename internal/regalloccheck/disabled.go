//go:build !wago_regalloccheck

package regalloccheck

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
