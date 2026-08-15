package shared

import "testing"

func TestCallTrafficAny(t *testing.T) {
	if (CallTraffic{}).Any() {
		t.Fatal("zero call traffic reported work")
	}
	if !(CallTraffic{RegisterArgumentMoves: 1}).Any() || !(CallTraffic{RegisterResultMoves: 1}).Any() {
		t.Fatal("nonzero call traffic was omitted")
	}
}
