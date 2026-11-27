// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_exception_handling

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

type instanceExceptionTagState struct {
	// The directory belongs to the instance arena.
	tagIdentityBase uintptr
	// Aliases of one local tag share a stable handle.
	tagExports map[int]*Tag
}

func (in *Instance) installNativeTagDirectory(base uintptr) {
	in.ensurePluginState().tagIdentityBase = base
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
	a.recordAttached(tag)
	return nil
}

func (a *tagImportAttachments) recordAttached(tag *Tag) {
	a.set.push(tag)
}

func (a *tagImportAttachments) detachAll() {
	a.set.drain((*Tag).detachImporter)
}

func (b *instanceBuilder) attachTagImports() error {
	if b.c.memoryDir != nil {
		for i, def := range b.c.memoryDir.exceptionTags() {
			if def.ImportKey == "" {
				continue
			}
			tag, ok := b.imports.tag(b.c.tagImportBindingKey(i))
			if !ok {
				return missingTagImportError(def.ImportKey)
			}
			if err := b.tagAttachments.attach(tag, def.TypeIndex, b.c.Types); err != nil {
				return fmt.Errorf("imported tag %q: %w", def.ImportKey, err)
			}
		}
	}
	return nil
}

func (b *instanceBuilder) rollbackTagImports(cleanup *resourceCleanup) {
	cleanup.run(b.tagAttachments.detachAll)
}

func detachImportedTags(in *Instance) {
	if in == nil || in.c == nil || in.c.memoryDir == nil {
		return
	}
	var cleanup resourceCleanup
	var seen importDedup[*Tag]
	importCount := in.c.tagImportCount()
	for i := 0; i < importCount; i++ {
		tag, ok := in.imports.tag(in.c.tagImportBindingKey(i))
		if ok && seen.add(tag) {
			cleanup.run(tag.detachImporter)
		}
	}
	cleanup.finish()
}

// ExportedTag returns the exact identity-bearing tag exported under name.
// Duplicate aliases of one tag return the same handle. Re-exports forward the
// original producer handle rather than manufacturing a consumer identity.
func (in *Instance) ExportedTag(name string) (*Tag, error) {
	if in == nil || in.c == nil || in.c.memoryDir == nil {
		return nil, fmt.Errorf("instance has no exported tag %q", name)
	}
	index, ok := in.c.memoryDir.exceptionTagExports()[name]
	if !ok || index < 0 || index >= len(in.c.memoryDir.exceptionTags()) {
		return nil, fmt.Errorf("no exported tag %q", name)
	}
	def := in.c.memoryDir.exceptionTags()[index]
	if def.ImportKey != "" {
		tag, ok := in.imports[in.c.tagImportBindingKey(index)].(*Tag)
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
	if state.tagIdentityBase == 0 {
		return nil, fmt.Errorf("tag owner instance has no native identity directory")
	}
	tag := &Tag{owner: in, index: index, typeIndex: def.TypeIndex, identity: binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(state.tagIdentityBase+uintptr(index*8))), 8))}
	state.tagExports[index] = tag
	return tag, nil
}
