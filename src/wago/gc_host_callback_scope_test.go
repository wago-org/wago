package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"strings"
	"testing"
)

func TestGCCallbackTokensRequireActiveScope(t *testing.T) {
	store := newReferenceStore(true)
	in := &Instance{refStore: store}
	scope := &hostCallScope{}
	scope.active.Store(4)
	for _, caller := range []instanceHostModule{
		{in: in},
		{in: in, scope: scope},
		{in: in, scope: scope, generation: 2},
		{in: &Instance{refStore: newReferenceStore(true)}, scope: scope, generation: 4},
	} {
		if _, err := store.issueCallbackGCRef(caller, gc.Ref(2), ValueTypeDescriptor{}); err == nil || !strings.Contains(err.Error(), "active host scope") {
			t.Fatalf("unscoped callback token error=%v", err)
		}
	}
}
