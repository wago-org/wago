// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"fmt"
	"sync"
)

// Tag is the identity-bearing handle for an instance-exported exception tag.
// Staged amd64 execution carries active handlers in RBP and compares these exact
// arena-backed identities across retained instance calls.
type Tag struct {
	mu        sync.Mutex
	owner     *Instance
	index     int
	typeIndex uint32
	identity  uint64
	importers int
}

func tagTypeEquivalent(actual uint32, actualTypes []DefinedTypeDescriptor, required uint32, requiredTypes []DefinedTypeDescriptor) bool {
	// definedTypeEquivalent compares the complete recursive groups containing
	// the requested members and already checks corresponding member positions.
	return definedTypeEquivalent(actual, actualTypes, required, requiredTypes)
}

func (t *Tag) identityValue() uint64 {
	if t == nil {
		return 0
	}
	return t.identity
}

func (t *Tag) validateImport(requiredType uint32, requiredTypes []DefinedTypeDescriptor) error {
	if t == nil || t.owner == nil || t.owner.c == nil || t.identity == 0 {
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

func (im resolvedImports) tag(key string) (*Tag, bool) {
	tag, ok := im[key].(*Tag)
	return tag, ok && tag != nil
}

func (c *Compiled) tagImportCount() int {
	if !tagImportsForImage || c == nil || c.memoryDir == nil {
		return 0
	}
	n := 0
	for _, tag := range c.memoryDir.exceptionTags() {
		if tag.ImportKey == "" {
			break
		}
		n++
	}
	return n
}

// TagImports returns imported exception-tag keys in declaration order.
func (c *Compiled) TagImports() []string {
	if c == nil || c.memoryDir == nil {
		return nil
	}
	out := make([]string, 0, len(c.memoryDir.exceptionTags()))
	for _, tag := range c.memoryDir.exceptionTags() {
		if tag.ImportKey == "" {
			break
		}
		out = append(out, tag.ImportKey)
	}
	return out
}
