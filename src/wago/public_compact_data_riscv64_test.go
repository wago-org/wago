//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
)

func TestRV64PublicCompactDataAdmission(t *testing.T) {
	c, err := Compile(nil, growMemModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := c.executionView()
	check := func(name string, data *compactActiveData, accepted bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			view := *base
			view.Data = nil
			view.compactData = data
			err := railshotValidateCompiled(&view)
			if accepted && err != nil || !accepted && !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("compact data admission", accepted, err)
			}
		})
	}
	check("empty", &compactActiveData{}, true)
	check("limit", &compactActiveData{records: make([]compactDataRecord, 256)}, true)
	// Count admission must precede reading any record payload, even if an
	// internal compact directory contains an invalid payload range.
	tooMany := make([]compactDataRecord, 257)
	tooMany[0].end = ^uint32(0)
	check("count-before-traversal", &compactActiveData{records: tooMany}, false)
	check("wrong-memory", &compactActiveData{records: []compactDataRecord{{memoryIndex: 1}}}, false)
	payload := make([]byte, railshot.MaxModuleBytes+1)
	check("payload-limit", &compactActiveData{records: []compactDataRecord{{end: railshot.MaxModuleBytes}}, bytes: payload}, true)
	check("payload-over-limit", &compactActiveData{records: []compactDataRecord{{end: railshot.MaxModuleBytes + 1}}, bytes: payload}, false)
	view := *base
	view.compactData = &compactActiveData{records: []compactDataRecord{{end: railshot.MaxModuleBytes}}, bytes: payload}
	view.PassiveData = []PassiveDataInit{{Bytes: []byte{1}}}
	if err := railshotValidateCompiled(&view); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("combined compact and passive payload bound", err)
	}
}
