// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || (darwin && arm64)) && !riscv64 && !tinygo && !wago_target_tinygo

package wago

// Enumerate only the exact, deduplicated attachments installed by construction.
// Unknown roots (host handles, tokens, construction, foreign stores) remain
// external. The joint domain lease freezes import topology; the lifetime epoch
// and final lifeMu claim reject a concurrent release/acquisition.
func managedFunctionHostImports(in *Instance, visit func(*Instance, func())) {
	if in == nil || in.c == nil {
		return
	}
	var functions importDedup[*Instance]
	for i := range in.c.Imports {
		export, ok := in.imports[in.c.functionImportBindingKey(i)].(*InstanceExport)
		if ok && export != nil && export.inst != nil && functions.add(export.inst) && !in.ownsTransferredFunctionAttachment(export.inst) {
			producer := export.inst
			visit(producer, func() { in.transferImportedFunctionAttachment(producer) })
		}
	}
	var globals importDedup[*Global]
	for i, imp := range in.c.GlobalImports {
		provided, ok := in.imports.global(in.c.globalImportBindingKey(i))
		if !ok || provided.Global == nil || (!isReferenceValType(imp.Type) && provided.Global.owner == nil) {
			continue
		}
		global := provided.Global
		if globals.add(global) && !in.ownsTransferredGlobalAttachment(global) {
			visit(global.instanceOwner(), func() { in.transferImportedGlobalAttachment(global) })
		}
	}
	var tables importDedup[*Table]
	for i := 0; i < in.c.tableImportCount(); i++ {
		table, ok := in.imports.table(in.c.tableImportBindingKey(i))
		if ok && table != nil && tables.add(table) && !in.ownsTransferredTableAttachment(table) {
			visit(table.instanceOwner(), func() { in.transferImportedTableAttachment(table) })
		}
	}
	var memories importDedup[*Memory]
	for i := 0; i < in.c.memoryImportCount(); i++ {
		memory, ok := in.imports[in.c.memoryImportBindingKey(i)].(*Memory)
		if !ok || memory == nil || !memories.add(memory) || in.ownsTransferredMemoryAttachment(memory) {
			continue
		}
		state := memory.state.Load()
		if state == nil {
			continue
		}
		state.mu.Lock()
		owner := state.owner
		state.mu.Unlock()
		visit(owner, func() { in.transferImportedMemoryAttachment(memory) })
	}
}
func (m *managedFunctionMark) snapshotHostDependencies() {
	for _, member := range m.graph.rows {
		if member == nil {
			continue
		}
		owner := member.owner
		owner.lifeMu.Lock()
		ready := !owner.resourcesClosed && !owner.constructionActive
		owner.lifeMu.Unlock()
		if !ready {
			continue
		}
		managedFunctionHostImports(owner, func(target *Instance, _ func()) {
			if to := m.graph.members[target]; to != nil {
				m.incoming[to]++
				m.edges[member] = append(m.edges[member], to)
			}
		})
	}
}

// The completed joint mark claims all dead owners before clearing their roots.
// Drain their import units outside collector/domain/topology locks, so arbitrary
// physical callbacks cannot deadlock or observe a partially claimed cycle.
func managedFunctionDrainDeadDependencies(g *managedFunctionGraph) {
	g.mu.Lock()
	var dead []*Instance
	for _, member := range g.rows {
		if member != nil && member.collecting.Load() {
			dead = append(dead, member.owner)
		}
	}
	g.mu.Unlock()
	var cleanup resourceCleanup
	for _, owner := range dead {
		managedFunctionHostImports(owner, func(_ *Instance, detach func()) { cleanup.run(detach) })
	}
	cleanup.finish()
}

func (m *managedFunctionMark) lockDependencySnapshot() func() {
	m.snapshotHostDependencies()
	return func() {}
}
func (*managedFunctionMark) lockDependencyCommit() (func(), bool) { return func() {}, true }
