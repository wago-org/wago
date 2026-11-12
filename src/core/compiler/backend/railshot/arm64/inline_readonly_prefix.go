// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

type inlineReadonlyPrefix struct {
	first, last nodeID
	count       int
}

// A fallthrough-only private tuple frame cannot reach the caller's controls or
// write its local homes. Keep a small prefix of immutable caller reads outside
// that frame's stack model so its internal joins need not spill those reads.
func (f *fn) detachInlineReadonlyPrefix() inlineReadonlyPrefix {
	var empty inlineReadonlyPrefix
	if f.inlineDepth != 1 || f.interruptible || f.intervalControl || f.selectGroupEnd != 0 || f.streamingReduction {
		return empty
	}
	prefix := inlineReadonlyPrefix{first: f.s.head.next, last: f.s.head.prev}
	for id := prefix.first; id != sentinelNodeID; {
		e := f.s.node(id)
		if prefix.count == 4 || e.elemKind() != ekValue || e.st.hasGCRoot() || e.st.hasEHRoot() || e.st.typ != mtI32 && e.st.typ != mtI64 {
			return empty
		}
		switch e.st.kind {
		case stConst:
		case stLocalReg:
			x := e.st.index()
			if x < 0 || x >= f.localBase || x >= f.nLocals || !f.pinnedLocalMask.has(e.st.reg) {
				return empty
			}
			local := f.locals[x]
			if local.reg != e.st.reg || local.isFloat || local.state == lsMem || local.state == lsConstZero {
				return empty
			}
		default:
			return empty
		}
		prefix.count++
		id = e.next
	}
	if prefix.count == 0 {
		return empty
	}
	// Do not recycle the detached nodes: arena IDs must stay live until restore.
	f.s.head.next, f.s.head.prev = sentinelNodeID, sentinelNodeID
	f.stats.peep("inline-tuple-readonly-prefix")
	return prefix
}

func (f *fn) restoreInlineReadonlyPrefix(prefix inlineReadonlyPrefix) {
	if prefix.count == 0 {
		return
	}
	first := f.s.head.next
	f.s.node(prefix.first).prev = sentinelNodeID
	f.s.node(prefix.last).next = first
	if first == sentinelNodeID {
		f.s.head.prev = prefix.last
	} else {
		f.s.node(first).prev = prefix.last
	}
	f.s.head.next = prefix.first
}
