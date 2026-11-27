// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_no_exception_handling

package wago

type compiledExceptionTagMetadata struct {
	tags    []compiledTagDef
	exports map[string]int
}

func (m *compiledExceptionTagMetadata) exceptionTags() []compiledTagDef { return m.tags }

func (m *compiledExceptionTagMetadata) exceptionTagExports() map[string]int { return m.exports }

func (m *compiledExceptionTagMetadata) hasExceptionTagMetadata() bool {
	return len(m.tags) != 0 || len(m.exports) != 0
}

func (m *compiledExceptionTagMetadata) appendExceptionTag(tag compiledTagDef) error {
	m.tags = append(m.tags, tag)
	return nil
}

func (m *compiledExceptionTagMetadata) setExceptionTagExport(name string, index int) error {
	if m.exports == nil {
		m.exports = make(map[string]int)
	}
	m.exports[name] = index
	return nil
}

func (w *compiledWriter) tags(c *Compiled) {
	if c.memoryDir == nil {
		w.uvar(0)
		w.stringIntMap(nil)
		return
	}
	_, _, _, tagModuleEnds, exactImportNames := c.importModuleEndSections()
	w.uvar(uint64(len(c.memoryDir.exceptionTags())))
	for i, tag := range c.memoryDir.exceptionTags() {
		w.str(tag.ImportKey)
		moduleEnd := uint64(0)
		if exactImportNames && tag.ImportKey != "" {
			moduleEnd = tagModuleEnds[i]
		}
		w.uvar(moduleEnd)
		w.u32(tag.TypeIndex)
	}
	w.stringIntMap(c.memoryDir.exceptionTagExports())
}
