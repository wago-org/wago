// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "fmt"

func (c *Compiled) validateCollectorHostImport(index int, key string, binding any, store *referenceStore, pluginImport bool) error {
	switch owner := binding.(type) {
	case *HostFuncRef:
		if owner == nil || !owner.allowsGCBridge() || store == nil || owner.store != store || c.genericGCFrameRoots() == nil {
			return fmt.Errorf("host import %q cannot transfer collector references; use Runtime.NewGCHostFuncRef, a Runtime plugin import, or a same-Runtime InstanceExport", key)
		}
		return nil
	case slotHostFunc:
		if owner == nil || !pluginImport || store == nil {
			return fmt.Errorf("host import %q cannot transfer collector references; use Runtime.NewGCHostFuncRef, a Runtime plugin import, or a same-Runtime InstanceExport", key)
		}
	case callerSlotHostFunc:
		if owner == nil || !pluginImport || store == nil {
			return fmt.Errorf("host import %q cannot transfer collector references; use a Runtime plugin import", key)
		}
	case HostCallFunc:
		if owner == nil || !pluginImport || store == nil {
			return fmt.Errorf("host import %q cannot transfer collector references; use a Runtime plugin import", key)
		}
	default:
		if !pluginRuntimeBuilt || !isGatedCollectorHostCallback(binding) {
			return fmt.Errorf("host import %q cannot transfer collector references; use Runtime.NewGCHostFuncRef, a Runtime plugin import, or a same-Runtime InstanceExport", key)
		}
		if !pluginImport || store == nil {
			return fmt.Errorf("host import %q cannot transfer collector references; use a Runtime plugin import", key)
		}
	}
	if c.importTransfersCollectorObjects(index) && c.genericGCFrameRoots() == nil {
		return fmt.Errorf("Runtime plugin host import %q cannot transfer collector references: exact native root maps are unavailable", key)
	}
	return nil
}
