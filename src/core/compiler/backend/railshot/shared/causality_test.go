package shared

import "testing"

func TestCallTrafficAny(t *testing.T) {
	if (CallTraffic{}).Any() {
		t.Fatal("zero call traffic reported work")
	}
	if !(CallTraffic{RegisterArgumentMoves: 1}).Any() || !(CallTraffic{RegisterResultMoves: 1}).Any() {
		t.Fatal("nonzero call traffic was omitted")
	}
	if !(CallTraffic{IntegerCallArgumentMoves: 1}).Any() || !(CallTraffic{MixedCallArgumentMoves: 1}).Any() || !(CallTraffic{TailCallArgumentMoves: 1}).Any() {
		t.Fatal("nonzero call-argument cause was omitted")
	}
}
