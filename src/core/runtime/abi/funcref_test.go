package abi

import "testing"

func TestFuncRefHomeEntryKindRoundTrip(t *testing.T) {
	const home = uint64(0x1234_5678)
	for _, kind := range []FuncRefEntryKind{
		FuncRefEntryInternal,
		FuncRefEntryLocalWrapper,
		FuncRefEntryCrossInstanceWrapper,
		FuncRefEntryHostThunk,
	} {
		tagged, ok := TagFuncRefHome(home, kind)
		if !ok {
			t.Fatalf("TagFuncRefHome(%v) rejected", kind)
		}
		gotKind, gotHome := DecodeFuncRefHome(tagged)
		if gotKind != kind || gotHome != home {
			t.Fatalf("round trip %v = (%v, %#x)", kind, gotKind, gotHome)
		}
	}
}

func TestFuncRefHomeEntryKindRejectsCollisions(t *testing.T) {
	if _, ok := TagFuncRefHome(FuncRefInternalHomeTag, FuncRefEntryHostThunk); ok {
		t.Fatal("tag-colliding home pointer accepted")
	}
	if _, ok := TagFuncRefHome(1, FuncRefEntryInvalid); ok {
		t.Fatal("invalid entry kind accepted")
	}
	for _, tags := range []uint64{
		FuncRefInternalHomeTag | FuncRefLocalWrapperHomeTag,
		FuncRefInternalHomeTag | FuncRefCrossInstanceHomeTag,
		FuncRefCrossInstanceHomeTag | FuncRefLocalWrapperHomeTag,
		FuncRefHomeTagMask,
	} {
		kind, home := DecodeFuncRefHome(tags | 0x1234)
		if kind != FuncRefEntryInvalid || home != 0x1234 {
			t.Fatalf("DecodeFuncRefHome(%#x) = (%v, %#x)", tags, kind, home)
		}
	}
}
