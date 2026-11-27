// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build !wago_standalone_no_collector && (!wago_no_gc || (linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_exception_handling))

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// gcFrameCodeDirectory locates live instances by saved native return PC.
type gcFrameCodeDirectory struct {
	codeRanges      *gcFrameCodeRangeIndex
	singleCodeOwner *Instance
}

const gcFrameCodeRangePageShift = 12

type gcFrameCodeRangeKey struct {
	collector *gc.Collector
	compiled  *Compiled
	base      uintptr
}

type gcFrameCodeRange struct {
	key    gcFrameCodeRangeKey
	owner  *Instance
	bytes  uintptr
	owners *gcFrameCodeOwnerLink
}

// gcFrameCodeOwnerLink tracks one instance sharing a compiled code image, so
// changing the representative owner never scans every live instance.
type gcFrameCodeOwnerLink struct {
	instance *Instance
	image    *gcFrameCodeRange
	prev     *gcFrameCodeOwnerLink
	next     *gcFrameCodeOwnerLink
}

type gcCollectorCodeRanges struct {
	byImage map[gcFrameCodeRangeKey]*gcFrameCodeRange
	byPage  map[uintptr][]*gcFrameCodeRange
}

type gcFrameCodeRangeIndex struct {
	byCollector map[*gc.Collector]*gcCollectorCodeRanges
	byInstance  map[*Instance]*gcFrameCodeOwnerLink
}

func (s *referenceStore) registerGCFrameCodeRangeLocked(in *Instance) {
	if s == nil || in == nil || in.gc == nil || in.c == nil || in.base == 0 || in.c.genericGCFrameRoots() == nil || len(in.c.code) == 0 {
		return
	}
	bytes := uintptr(len(in.c.code))
	if in.base > ^uintptr(0)-bytes {
		return
	}
	topology := s.ensureGCTopologyLocked()
	if topology.codeRanges == nil {
		// A single GC code owner needs no hash tables or ownership links. This
		// is the common first-instantiation case; promote when a second arrives.
		first := topology.singleCodeOwner
		if first == nil || first == in {
			topology.singleCodeOwner = in
			return
		}
		topology.singleCodeOwner = nil
		topology.codeRanges = &gcFrameCodeRangeIndex{
			byCollector: make(map[*gc.Collector]*gcCollectorCodeRanges),
		}
		s.registerGCFrameCodeRangeLocked(first)
	}
	if topology.codeRanges.byInstance == nil {
		topology.codeRanges.byInstance = make(map[*Instance]*gcFrameCodeOwnerLink)
	}
	if topology.codeRanges.byInstance[in] != nil {
		return
	}
	link := &gcFrameCodeOwnerLink{instance: in}
	topology.codeRanges.byInstance[in] = link
	collector := topology.codeRanges.byCollector[in.gc]
	if collector == nil {
		collector = &gcCollectorCodeRanges{byImage: make(map[gcFrameCodeRangeKey]*gcFrameCodeRange), byPage: make(map[uintptr][]*gcFrameCodeRange)}
		topology.codeRanges.byCollector[in.gc] = collector
	}
	key := gcFrameCodeRangeKey{collector: in.gc, compiled: in.c, base: in.base}
	if image := collector.byImage[key]; image != nil {
		link.instance, link.image, link.next = in, image, image.owners
		if image.owners != nil {
			image.owners.prev = link
		}
		image.owners, image.owner = link, in
		return
	}
	image := &gcFrameCodeRange{key: key, owner: in, bytes: bytes}
	link.instance, link.image = in, image
	image.owners = link
	collector.byImage[key] = image
	firstPage, lastPage := in.base>>gcFrameCodeRangePageShift, (in.base+bytes-1)>>gcFrameCodeRangePageShift
	for page := firstPage; ; page++ {
		collector.byPage[page] = append(collector.byPage[page], image)
		if page == lastPage {
			break
		}
	}
}

func (s *referenceStore) unregisterGCFrameCodeRangeLocked(in *Instance) {
	if s == nil || in == nil {
		return
	}
	topology := s.gcTopologyLocked()
	if topology == nil {
		return
	}
	if topology.singleCodeOwner == in {
		topology.singleCodeOwner = nil
		return
	}
	if topology.codeRanges == nil || in.gc == nil || in.c == nil || in.base == 0 {
		return
	}
	collector := topology.codeRanges.byCollector[in.gc]
	if collector == nil {
		return
	}
	key := gcFrameCodeRangeKey{collector: in.gc, compiled: in.c, base: in.base}
	image := collector.byImage[key]
	if image == nil {
		return
	}
	link := topology.codeRanges.byInstance[in]
	if s.instances[in] == nil || link == nil || link.image != image {
		return
	}
	if link.prev == nil {
		image.owners = link.next
	} else {
		link.prev.next = link.next
	}
	if link.next != nil {
		link.next.prev = link.prev
	}
	delete(topology.codeRanges.byInstance, in)
	image.owner = nil
	if image.owners != nil {
		image.owner = image.owners.instance
		return
	}
	delete(collector.byImage, key)
	firstPage, lastPage := in.base>>gcFrameCodeRangePageShift, (in.base+image.bytes-1)>>gcFrameCodeRangePageShift
	for page := firstPage; ; page++ {
		images := collector.byPage[page]
		for i, candidate := range images {
			if candidate == image {
				images[i] = images[len(images)-1]
				images[len(images)-1] = nil
				images = images[:len(images)-1]
				break
			}
		}
		if len(images) == 0 {
			delete(collector.byPage, page)
		} else {
			collector.byPage[page] = images
		}
		if page == lastPage {
			break
		}
	}
	if len(collector.byImage) == 0 {
		delete(topology.codeRanges.byCollector, in.gc)
	}
	if len(topology.codeRanges.byCollector) == 0 && len(topology.codeRanges.byInstance) == 0 {
		topology.codeRanges = nil
	}
}

func (s *referenceStore) gcFrameOwner(pc uintptr, collector *gc.Collector) *Instance {
	if s == nil || collector == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if topology := s.gcTopologyLocked(); topology != nil && topology.singleCodeOwner != nil {
		owner := topology.singleCodeOwner
		if owner.gc == collector && pc >= owner.base && pc-owner.base < uintptr(len(owner.c.code)) {
			return owner
		}
		return nil
	}
	if topology := s.gcTopologyLocked(); topology != nil && topology.codeRanges != nil {
		if ranges := topology.codeRanges.byCollector[collector]; ranges != nil {
			for _, image := range ranges.byPage[pc>>gcFrameCodeRangePageShift] {
				if pc >= image.key.base && pc-image.key.base < image.bytes {
					return image.owner
				}
			}
		}
		return nil
	}
	for candidate, state := range s.instances {
		if state == nil || state.resourcesReleased || candidate == nil || candidate.gc != collector || candidate.c == nil || candidate.c.genericGCFrameRoots() == nil {
			continue
		}
		if pc >= candidate.base && pc-candidate.base < uintptr(len(candidate.c.code)) {
			return candidate
		}
	}
	return nil
}
