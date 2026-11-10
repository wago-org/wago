// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package hostthunk_test

import (
	"bytes"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/runtime/hostthunk"
)

func TestAMD64MatchesCompilerThunks(t *testing.T) {
	for _, test := range []struct {
		name string
		got  []byte
		want []byte
	}{
		{name: "indirect", got: hostthunk.Indirect(7), want: railshot.HostIndirectThunk(7)},
		{name: "sync", got: hostthunk.IndirectSync(7, 3, 2), want: railshot.HostIndirectSyncThunk(7, 3, 2)},
		{name: "owned sync", got: hostthunk.IndirectOwnedSync(7, 3, 2), want: railshot.HostIndirectOwnedSyncThunk(7, 3, 2)},
		{name: "wide parameter sync", got: hostthunk.IndirectSync(7, 65, 2), want: railshot.HostIndirectSyncThunk(7, 65, 2)},
		{name: "wide result sync", got: hostthunk.IndirectSync(7, 3, 65), want: railshot.HostIndirectSyncThunk(7, 3, 65)},
		{name: "wide parameter owned sync", got: hostthunk.IndirectOwnedSync(7, 65, 2), want: railshot.HostIndirectOwnedSyncThunk(7, 65, 2)},
		{name: "wide result owned sync", got: hostthunk.IndirectOwnedSync(7, 3, 65), want: railshot.HostIndirectOwnedSyncThunk(7, 3, 65)},
	} {
		if !bytes.Equal(test.got, test.want) {
			t.Errorf("%s runtime thunk differs from compiler thunk", test.name)
		}
	}
}
