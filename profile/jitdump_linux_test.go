// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && !tinygo

package profile

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func TestJITDumpDiscoveryMapping(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJITDump(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.RefreshMarker(); err != nil {
		t.Fatal(err)
	}
	events := fixture()
	events[0].Timestamp = jitprofile.Now()
	if err = j.Write(events); err != nil {
		t.Fatal(err)
	}
	if len(j.marker) == 0 {
		t.Fatal("missing perf discovery mapping")
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("jit-%d.dump", os.Getpid())))
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(b[len(b)-16:]) != 3 {
		t.Fatal("missing terminal close")
	}
}
