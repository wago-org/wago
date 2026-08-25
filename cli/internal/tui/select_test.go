package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func newTestSelect() *MultiSelect {
	return &MultiSelect{
		Title: "pick",
		Items: []SelectItem{
			{Label: "wasi:stdio", On: true},
			{Label: "wasi:clock", On: false},
			{Label: "env:args", On: true},
		},
	}
}

func TestMultiSelectFrame(t *testing.T) {
	m := newTestSelect()
	text := m.frame()
	for _, want := range []string{"pick", "› ", "◉", "○", "wasi:stdio", "enter/→ accept"} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[x]") || strings.Contains(text, "[ ]") {
		t.Fatalf("frame uses legacy checkbox glyphs:\n%s", text)
	}
	m.Title = ""
	m.Prompt = "custom prompt"
	m.Items = []SelectItem{{Label: "x", Description: "description"}}
	if text := m.frame(); strings.Contains(text, "pick") || !strings.Contains(text, "description") || !strings.Contains(text, "custom prompt") {
		t.Fatalf("custom frame = %q", text)
	}
}

func TestMultiSelectFrameGroupsAndIndentsRows(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &MultiSelect{Items: []SelectItem{
		{Label: "host.arguments.read", Group: "github.com/wago-org/wasi", On: true},
		{Label: "host.import.define", Group: "github.com/wago-org/wasi", On: true},
		{Label: "instance.manage", Group: "github.com/wago-org/workers", On: true},
		{Label: "Reject all", Reject: true},
	}}
	frame := m.Frame()
	for _, want := range []string{
		"github.com/wago-org/wasi\n  › ◉ host.arguments.read",
		"github.com/wago-org/workers\n    ◉ instance.manage",
		"\n  ○ Reject all",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("grouped frame missing %q:\n%s", want, frame)
		}
	}
	if strings.Count(frame, "github.com/wago-org/wasi\n") != 1 {
		t.Fatalf("plugin heading repeated:\n%s", frame)
	}
}

func TestMultiSelectMovementClamps(t *testing.T) {
	m := newTestSelect()
	m.apply(keyUp) // already at top
	if m.Cursor != 0 {
		t.Fatalf("cursor should clamp at 0, got %d", m.Cursor)
	}
	m.apply(keyDown)
	m.apply(keyDown)
	m.apply(keyDown) // past the end
	if m.Cursor != 2 {
		t.Fatalf("cursor should clamp at last index 2, got %d", m.Cursor)
	}
}

func TestMultiSelectToggleAllNone(t *testing.T) {
	m := newTestSelect()
	m.apply(keyDown)   // cursor -> wasi:clock
	m.apply(keyToggle) // turn it on
	if got := m.Chosen(); !reflect.DeepEqual(got, []string{"wasi:stdio", "wasi:clock", "env:args"}) {
		t.Fatalf("after toggle: %v", got)
	}
	m.apply(keyClear)
	if got := m.Chosen(); got != nil {
		t.Fatalf("keyClear should deselect all, got %v", got)
	}
	m.apply(keyAll)
	if got := m.Chosen(); len(got) != 3 {
		t.Fatalf("keyAll should select all, got %v", got)
	}
	m.apply(keyAll)
	if got := m.Chosen(); got != nil {
		t.Fatalf("keyAll should clear an entirely selected list, got %v", got)
	}
}

func TestMultiSelectDisabledPreviewCannotBeSelected(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{
		{Label: "tail-calls", Description: "planned", Disabled: true},
		{Label: "inline", Description: "available"},
	}}
	m.apply(keyToggle)
	if m.Items[0].On {
		t.Fatal("space selected a disabled preview")
	}
	m.apply(keyAll)
	if m.Items[0].On || !m.Items[1].On {
		t.Fatalf("select all changed disabled row or skipped selectable row: %#v", m.Items)
	}
	if got := m.Chosen(); !reflect.DeepEqual(got, []string{"inline"}) {
		t.Fatalf("chosen = %v, want [inline]", got)
	}
	if frame := m.frame(); !strings.Contains(frame, "◌") || !strings.Contains(frame, "tail-calls") {
		t.Fatalf("disabled preview is not rendered distinctly:\n%s", frame)
	}
}

func TestMultiSelectFixedRowCannotBeSelected(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &MultiSelect{Items: []SelectItem{{Label: "required", On: true, Fixed: true}}}
	m.apply(keyToggle)
	if !m.Items[0].On || !strings.Contains(m.Frame(), "✓") {
		t.Fatalf("fixed row = %#v\n%s", m.Items[0], m.Frame())
	}
}

func TestMultiSelectActionRowSubmitsWithoutPermissionMark(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &MultiSelect{Items: []SelectItem{{Label: "Install 2 plugins", Action: true}}}
	if done, cancelled := m.apply(keyToggle); !done || cancelled {
		t.Fatalf("action row = done %v, cancelled %v", done, cancelled)
	}
	if frame := m.Frame(); !strings.Contains(frame, "[ Install 2 plugins ]") || strings.Contains(frame, "○ Install 2 plugins") {
		t.Fatalf("action row frame:\n%s", frame)
	}
}

func TestMultiSelectWrapsDescriptionsWithinRedrawWidth(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &MultiSelect{Items: []SelectItem{{
		Label:       "core.instance.instantiate",
		Description: "instantiate and own the bounded core-module graph behind a component instance · component-model · limit: 64 instances · 20 GiB memory",
	}}, Prompt: "space details · enter install · esc cancel"}
	for _, line := range strings.Split(strings.TrimSuffix(m.Frame(), "\n"), "\n") {
		if len(line) > 78 {
			t.Fatalf("selector line exceeds redraw width (%d): %q", len(line), line)
		}
	}
}

func TestMultiSelectConfirmOffSubmitsImmediately(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{{Label: "required", On: true, ConfirmOff: true}}}
	done, cancelled := m.apply(keyToggle)
	if !done || cancelled || m.Items[0].On {
		t.Fatalf("required toggle = done %v, cancelled %v, row %#v", done, cancelled, m.Items[0])
	}
}

func TestMultiSelectAcceptCancel(t *testing.T) {
	m := newTestSelect()
	if done, cancelled := m.apply(keyAccept); !done || cancelled {
		t.Fatalf("enter => done, not cancelled; got done=%v cancelled=%v", done, cancelled)
	}
	if done, cancelled := m.apply(keyCancel); !done || !cancelled {
		t.Fatalf("esc => done and cancelled; got done=%v cancelled=%v", done, cancelled)
	}
	if done, cancelled := m.apply(keyLeft); !done || !cancelled {
		t.Fatalf("left => done and cancelled; got done=%v cancelled=%v", done, cancelled)
	}
	if done, _ := m.apply(keyNoop); done {
		t.Fatalf("noop must not finish the interaction")
	}
}

func TestMultiSelectRejectKey(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{{Label: "a", On: true}, {Label: "b", On: true}, {Label: "Reject all", Reject: true}}}
	// r clears everything and submits (grant nothing), and is NOT a cancel.
	done, cancelled := m.apply(keyReject)
	if !done || cancelled {
		t.Fatalf("r => done, not cancelled; got done=%v cancelled=%v", done, cancelled)
	}
	if got := m.Chosen(); len(got) != 0 {
		t.Fatalf("r must clear all selections, got %v", got)
	}
	if !m.Rejected() {
		t.Fatal("r did not select the explicit reject row")
	}
}

func TestMultiSelectRejectRowIsExclusive(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{
		{Label: "required", On: true},
		{Label: "optional", On: true},
		{Label: "Reject all", Reject: true},
	}, Cursor: 2}
	if done, cancelled := m.apply(keyToggle); !done || cancelled {
		t.Fatalf("reject row should submit immediately: done=%v cancelled=%v", done, cancelled)
	}
	if got := m.Chosen(); got != nil || !m.Rejected() {
		t.Fatalf("reject row left grants selected: chosen=%v rejected=%v", got, m.Rejected())
	}
	m.Cursor = 0
	m.apply(keyToggle)
	if got := m.Chosen(); !reflect.DeepEqual(got, []string{"required"}) || m.Rejected() {
		t.Fatalf("grant row did not clear rejection: chosen=%v rejected=%v", got, m.Rejected())
	}
}

func TestMultiSelectEnterOnRejectRowRejects(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{
		{Label: "required", On: true},
		{Label: "Reject all", Reject: true},
	}, Cursor: 1}
	done, cancelled := m.apply(keyAccept)
	if !done || cancelled || !m.Rejected() || m.Items[0].On {
		t.Fatalf("enter on Reject all = done %v, cancelled %v, items %#v", done, cancelled, m.Items)
	}
}

func TestMultiSelectEnterAccepts(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{{Label: "a", On: true}, {Label: "b", On: false}}}
	// Enter on an ordinary row submits the checked items.
	m.apply(keyDown) // cursor on the unchecked "b"
	done, cancelled := m.apply(keyAccept)
	if !done || cancelled {
		t.Fatalf("enter => submit, not cancel; got done=%v cancelled=%v", done, cancelled)
	}
	if got := m.Chosen(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("chosen=%v, want [a]", got)
	}
}

func TestMultiSelectEscCancels(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{{Label: "a", On: true}}}
	if done, cancelled := m.apply(keyCancel); !done || !cancelled {
		t.Fatalf("esc => done + cancelled; got done=%v cancelled=%v", done, cancelled)
	}
}

func TestDecodeKey(t *testing.T) {
	cases := []struct {
		in   []byte
		want selectKey
	}{
		{[]byte{'\r'}, keyAccept},
		{[]byte{'\n'}, keyAccept},
		{[]byte{' '}, keyToggle},
		{[]byte{'a'}, keyAll},
		{[]byte{'n'}, keyClear},
		{[]byte{'r'}, keyReject},
		{[]byte{'c'}, keyCopy},
		{[]byte{'o'}, keyOpen},
		{[]byte{'q'}, keyQuit},
		{[]byte{3}, keyQuit},    // Ctrl-C
		{[]byte{27}, keyCancel}, // bare ESC
		{[]byte{'j'}, keyDown},
		{[]byte{'k'}, keyUp},
		{[]byte{'h'}, keyLeft},
		{[]byte{'<'}, keyLeft},
		{[]byte{'l'}, keyRight},
		{[]byte{'>'}, keyRight},
		{[]byte{27, '[', 'A'}, keyUp},
		{[]byte{27, '[', 'B'}, keyDown},
		{[]byte{27, '[', 'C'}, keyRight},
		{[]byte{27, '[', 'D'}, keyLeft},
		{[]byte{'x'}, keyNoop},
		{nil, keyNoop},
	}
	for _, tc := range cases {
		if got := decodeKey(tc.in); got != tc.want {
			t.Errorf("decodeKey(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestActionPrompt(t *testing.T) {
	for _, test := range []struct {
		key       selectKey
		action    Action
		cancelled bool
	}{
		{keyCopy, ActionCopy, false},
		{keyOpen, ActionOpen, false},
		{keyAccept, ActionContinue, false},
		{keyCancel, ActionNone, true},
	} {
		prompt := &ActionPrompt{Text: "Authorize on GitHub"}
		done, cancelled := prompt.apply(test.key)
		if !done || cancelled != test.cancelled || prompt.Action() != test.action {
			t.Fatalf("action %d = done %v, cancelled %v, action %d", test.key, done, cancelled, prompt.Action())
		}
	}
	frame := (&ActionPrompt{Text: "Authorize on GitHub"}).Frame()
	for _, want := range []string{"Authorize on GitHub", "c copy code", "o open browser", "enter continue", "esc cancel"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("action frame missing %q:\n%s", want, frame)
		}
	}
}

func TestMultiSelectRightSubmitsAndAllSelectsEveryItem(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{{Label: "canary"}, {Label: "nightly"}}}
	if done, cancelled := m.apply(keyAll); done || cancelled {
		t.Fatalf("select all = done %v, cancelled %v", done, cancelled)
	}
	if got := strings.Join(m.Chosen(), ","); got != "canary,nightly" {
		t.Fatalf("chosen after all = %q", got)
	}
	if done, cancelled := m.apply(keyRight); !done || cancelled {
		t.Fatalf("right submit = done %v, cancelled %v", done, cancelled)
	}
}

func TestMultiSelectPaginatesAtWindowBoundary(t *testing.T) {
	items := make([]SelectItem, pickerVisibleRows+2)
	for i := range items {
		items[i].Label = fmt.Sprintf("version-%02d", i)
	}
	m := &MultiSelect{Title: "Versions", Items: items}
	for range pickerVisibleRows - 1 {
		m.apply(keyDown)
	}
	if frame := m.frame(); !strings.Contains(frame, "version-00") || strings.Contains(frame, "version-15") {
		t.Fatalf("multi-select changed page early:\n%s", frame)
	}
	m.apply(keyDown)
	if frame := m.frame(); strings.Contains(frame, "version-14") || !strings.Contains(frame, "version-15") || !strings.Contains(frame, "↑ 15 more") {
		t.Fatalf("multi-select did not page at boundary:\n%s", frame)
	}
}

func TestMultiSelectExpandsAndEditsNestedItems(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &MultiSelect{Items: []SelectItem{{
		Label:       "host.arguments.read",
		Description: "Read guest arguments.\nused by: wasi/p1 · wasi/p2",
		Children: []SelectItem{
			{Label: "wasi/p1", On: true, Fixed: true, Description: "required"},
			{Label: "wasi/p2", Description: "optional"},
		},
	}}}
	if strings.Contains(m.Frame(), "○ wasi/p2") {
		t.Fatalf("collapsed selector renders package rows:\n%s", m.Frame())
	}
	if done, cancelled := m.apply(keyRight); done || cancelled || !m.Items[0].Expanded {
		t.Fatalf("right should expand: done=%v cancelled=%v item=%#v", done, cancelled, m.Items[0])
	}
	frame := m.Frame()
	for _, want := range []string{"used by: wasi/p1 · wasi/p2", "✓ wasi/p1", "○ wasi/p2"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("expanded selector missing %q:\n%s", want, frame)
		}
	}
	m.apply(keyDown) // required package
	m.apply(keyDown) // optional package
	if done, cancelled := m.apply(keyRight); done || cancelled {
		t.Fatalf("right on a nested package row should not submit: done=%v cancelled=%v", done, cancelled)
	}
	m.apply(keyToggle)
	if !m.Items[0].Children[1].On || !m.Items[0].On || m.Items[0].Partial {
		t.Fatalf("individual package toggle = %#v", m.Items[0])
	}
	m.apply(keyLeft)
	if m.Cursor != 0 {
		t.Fatalf("left should return to parent, cursor=%d", m.Cursor)
	}
}

func TestMultiSelectParentToggleKeepsRequiredNestedItem(t *testing.T) {
	m := &MultiSelect{Items: []SelectItem{{Label: "authority", Children: []SelectItem{
		{Label: "required", On: true, Fixed: true},
		{Label: "optional", On: false},
	}}}}
	m.rows() // synchronize initial aggregate state
	if !m.Items[0].Partial || m.Items[0].On {
		t.Fatalf("initial aggregate state = %#v", m.Items[0])
	}
	m.apply(keyToggle)
	if !m.Items[0].Children[1].On || !m.Items[0].On || m.Items[0].Partial {
		t.Fatalf("parent did not enable optional item: %#v", m.Items[0])
	}
	m.apply(keyToggle)
	if m.Items[0].Children[1].On || !m.Items[0].Children[0].On || !m.Items[0].Partial {
		t.Fatalf("parent disabled required item or lost partial state: %#v", m.Items[0])
	}
}

func TestPermissionFormUsesCompactWrappingViewport(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	items := make([]SelectItem, 6)
	for index := range items {
		items[index] = SelectItem{Label: fmt.Sprintf("authority-%d", index)}
	}
	form := NewPermissionForm("Permissions for wago-org/example", items)
	if !strings.Contains(form.Frame(), "authority-0") || strings.Contains(form.Frame(), "authority-5") {
		t.Fatalf("permission form did not limit the first viewport:\n%s", form.Frame())
	}
	form.apply(keyUp)
	if form.Cursor != len(items)-1 || !strings.Contains(form.Frame(), "authority-5") {
		t.Fatalf("permission form did not wrap upward: cursor=%d\n%s", form.Cursor, form.Frame())
	}
	form.apply(keyDown)
	if form.Cursor != 0 {
		t.Fatalf("permission form did not wrap downward: cursor=%d", form.Cursor)
	}
}

func TestPermissionFormCancelRowCancels(t *testing.T) {
	form := NewPermissionForm("Permissions", []SelectItem{{Label: "authority"}, {Label: "Cancel installation", Cancel: true}})
	form.Cursor = 1
	if done, cancelled := form.apply(keyAccept); !done || !cancelled {
		t.Fatalf("cancel row = done=%v cancelled=%v", done, cancelled)
	}
	if frame := form.Frame(); !strings.Contains(frame, "› Cancel installation") || strings.Contains(frame, "○ Cancel installation") {
		t.Fatalf("cancel row should be plain:\n%s", frame)
	}
}
