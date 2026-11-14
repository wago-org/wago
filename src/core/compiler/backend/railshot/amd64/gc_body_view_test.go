//go:build amd64

package amd64

import (
	"crypto/sha256"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"strings"
	"testing"
)

func TestGCBodyViewCertificates(t *testing.T) {
	for _, kind := range []string{"valid", "stale", "nonboundary", "type", "sink", "sink-read", "origins"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte{0x41, 1, 0xd0, 0x6d, 0xfb, 0, 0, 0x21, 0, 0xb}
			m := &wasm.Module{Code: []wasm.Func{{BodyBytes: body}}}
			view := codegen.GCBodyView{BodySHA256: sha256.Sum256(body), Constructors: map[uint32]codegen.GCDeadConstructor{4: {Type: 0, Sink: 0}}, Origins: []codegen.GCBodyOrigin{{NewPC: 0, OldFunction: 7, OldPC: 1}}}
			want := ""
			switch kind {
			case "stale":
				view.BodySHA256[0] ^= 1
				want = "stale"
			case "nonboundary":
				delete(view.Constructors, 4)
				view.Constructors[5] = codegen.GCDeadConstructor{}
				want = "instruction boundary"
			case "type":
				view.Constructors[4] = codegen.GCDeadConstructor{Type: 1}
				want = "not its constructor"
			case "sink":
				view.Constructors[4] = codegen.GCDeadConstructor{Sink: 1}
				want = "sink mismatch"
			case "sink-read":
				m.Code[0].BodyBytes = append(append([]byte(nil), body[:len(body)-1]...), 0x20, 0, 0x1a, 0xb)
				view.BodySHA256 = sha256.Sum256(m.Code[0].BodyBytes)
				want = "sink local is read"
			case "origins":
				view.Origins = append(view.Origins, view.Origins[0])
				want = "unordered source origins"
			}
			e := validateGCBodyViews(m, map[uint32]codegen.GCBodyView{0: view})
			if want == "" && e != nil {
				t.Fatal(e)
			}
			if want != "" && (e == nil || !strings.Contains(e.Error(), want)) {
				t.Fatalf("error%v want%s", e, want)
			}
		})
	}
}
func TestGCBodyViewSourcePC(t *testing.T) {
	f := fn{traceFuncIdx: 7, tracePCBase: 10, gcBodyViews: map[uint32]codegen.GCBodyView{7: {Origins: []codegen.GCBodyOrigin{{NewPC: 0, OldFunction: 7, OldPC: 5, Linear: true}, {NewPC: 3, OldFunction: 456, OldPC: 9}, {NewPC: 8, OldFunction: 7, OldPC: 20, Linear: true}}}}}
	for _, x := range []struct{ pc, fun, old uint32 }{{10, 7, 5}, {12, 7, 7}, {13, 456, 9}, {17, 456, 9}, {18, 7, 20}, {22, 7, 24}} {
		f.wasmPC = x.pc
		fun, old := f.gcBodySourcePC()
		if fun != x.fun || old != x.old {
			t.Fatalf("pc%d got%d/%d want%d/%d", x.pc, fun, old, x.fun, x.old)
		}
	}
}
