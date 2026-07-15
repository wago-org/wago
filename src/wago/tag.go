package wago

import (
	"fmt"
	"sync"
)

// Tag is the identity-bearing handle for an instance-exported exception tag.
// It is currently a declaration/link product only: staged compilation rejects
// every module that would throw through an imported/exported tag until native
// handler transfer across instance basedata is proven.
type Tag struct {
	mu        sync.Mutex
	owner     *Instance
	index     int
	typeIndex uint32
	importers int
}

func tagTypeEquivalent(actual uint32, actualTypes []DefinedTypeDescriptor, required uint32, requiredTypes []DefinedTypeDescriptor) bool {
	group := func(index uint32, types []DefinedTypeDescriptor) (start, end uint32, ok bool) {
		if int(index) >= len(types) {
			return 0, 0, false
		}
		id := types[index].RecGroup
		start, end = index, index+1
		for start > 0 && types[start-1].RecGroup == id {
			start--
		}
		for int(end) < len(types) && types[end].RecGroup == id {
			end++
		}
		return start, end, true
	}
	aStart, aEnd, aOK := group(actual, actualTypes)
	bStart, bEnd, bOK := group(required, requiredTypes)
	if !aOK || !bOK || aEnd-aStart != bEnd-bStart || actual-aStart != required-bStart {
		return false
	}
	for i := uint32(0); i < aEnd-aStart; i++ {
		if !definedTypeEquivalent(aStart+i, actualTypes, bStart+i, requiredTypes) {
			return false
		}
	}
	return true
}

func (t *Tag) validateImport(requiredType uint32, requiredTypes []DefinedTypeDescriptor) error {
	if t == nil || t.owner == nil || t.owner.c == nil {
		return fmt.Errorf("tag handle is invalid")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.owner.lifeMu.Lock()
	closed := t.owner.closed || t.owner.resourcesClosed
	t.owner.lifeMu.Unlock()
	if closed {
		return fmt.Errorf("tag owner instance is closed")
	}
	if !tagTypeEquivalent(t.typeIndex, t.owner.c.Types, requiredType, requiredTypes) {
		return fmt.Errorf("tag type is incompatible with required structural type")
	}
	return nil
}

func (t *Tag) attachImporter(requiredType uint32, requiredTypes []DefinedTypeDescriptor) error {
	if err := t.validateImport(requiredType, requiredTypes); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.owner.retainResourceRoot() {
		return fmt.Errorf("tag owner instance is closed")
	}
	t.importers++
	return nil
}

func (t *Tag) detachImporter() {
	if t == nil || t.owner == nil {
		return
	}
	t.mu.Lock()
	if t.importers == 0 {
		t.mu.Unlock()
		return
	}
	t.importers--
	owner := t.owner
	t.mu.Unlock()
	owner.releaseResourceRoot()
}

func (im Imports) tag(key string) (*Tag, bool) {
	tag, ok := im[key].(*Tag)
	return tag, ok && tag != nil
}

type tagImportAttachments struct {
	set importDedup[*Tag]
}

func (a *tagImportAttachments) attach(tag *Tag, requiredType uint32, requiredTypes []DefinedTypeDescriptor) error {
	if tag == nil {
		return fmt.Errorf("tag owner is nil")
	}
	if a.set.contains(tag) {
		return tag.validateImport(requiredType, requiredTypes)
	}
	if err := tag.attachImporter(requiredType, requiredTypes); err != nil {
		return err
	}
	a.set.push(tag)
	return nil
}

func (a *tagImportAttachments) detachAll() {
	a.set.each((*Tag).detachImporter)
	a.set.reset()
}

func (c *Compiled) tagImportCount() int {
	if c == nil || c.memoryDir == nil {
		return 0
	}
	n := 0
	for _, tag := range c.memoryDir.ehTags {
		if tag.ImportKey == "" {
			break
		}
		n++
	}
	return n
}

// ExportedTag returns the exact identity-bearing tag exported under name.
// Duplicate aliases of one tag return the same handle. Re-exports forward the
// original producer handle rather than manufacturing a consumer identity.
func detachImportedTags(in *Instance) {
	if in == nil || in.c == nil || in.c.memoryDir == nil {
		return
	}
	var seen importDedup[*Tag]
	for i := 0; i < in.c.tagImportCount(); i++ {
		def := in.c.memoryDir.ehTags[i]
		tag, ok := in.imports.tag(def.ImportKey)
		if ok && seen.add(tag) {
			tag.detachImporter()
		}
	}
}

func (in *Instance) ExportedTag(name string) (*Tag, error) {
	if in == nil || in.c == nil || in.c.memoryDir == nil {
		return nil, fmt.Errorf("instance has no exported tag %q", name)
	}
	index, ok := in.c.memoryDir.ehTagExports[name]
	if !ok || index < 0 || index >= len(in.c.memoryDir.ehTags) {
		return nil, fmt.Errorf("no exported tag %q", name)
	}
	def := in.c.memoryDir.ehTags[index]
	if def.ImportKey != "" {
		tag, ok := in.imports[def.ImportKey].(*Tag)
		if !ok || tag == nil {
			return nil, fmt.Errorf("exported tag %q imported binding is invalid", name)
		}
		return tag, nil
	}
	state := in.ensurePluginState()
	in.lifeMu.Lock()
	defer in.lifeMu.Unlock()
	if in.closed || in.resourcesClosed {
		return nil, fmt.Errorf("tag owner instance is closed")
	}
	if state.tagExports == nil {
		state.tagExports = make(map[int]*Tag)
	}
	if tag := state.tagExports[index]; tag != nil {
		return tag, nil
	}
	tag := &Tag{owner: in, index: index, typeIndex: def.TypeIndex}
	state.tagExports[index] = tag
	return tag, nil
}
