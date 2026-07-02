package gc

import "testing"

func TestRefEncoding(t *testing.T) {
	if !Null().IsNull() || Null().IsObj() || Null().IsI31() {
		t.Fatal("bad null")
	}
	for _, v := range []int32{0, 1, -1, 1<<30 - 1, -(1 << 30)} {
		r := I31New(v)
		if !r.IsI31() || r.IsObj() || r.IsNull() {
			t.Fatalf("bad i31 tag for %d", v)
		}
		if got := r.I31GetS(); got != v {
			t.Fatalf("I31GetS(%d)=%d", v, got)
		}
		if got := r.I31GetU(); got != uint32(v)&0x7fffffff {
			t.Fatalf("I31GetU(%d)=%#x", v, got)
		}
	}
	obj := makeObjRef(7)
	if !obj.IsObj() || obj.IsI31() || obj.IsNull() {
		t.Fatal("bad object ref")
	}
	if !RefEq(obj, makeObjRef(7)) || RefEq(obj, makeObjRef(8)) || !RefEq(Null(), Null()) {
		t.Fatal("bad ref equality")
	}
}
