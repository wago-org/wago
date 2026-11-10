// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"bytes"
	"sync"
	"testing"
)

func TestStructuralCallIdentityConcurrentPublication(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	defer c.Close()
	want, err := compiledStructuralCallIdentity(c, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.prepareStructuralCallIdentities(); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for j := 0; j < 1000; j++ {
				if got, ok := c.cachedStructuralCallIdentity(0); ok && !bytes.Equal(got, want) {
					t.Error("partially published structural identity")
				}
			}
		}()
	}
	close(start)
	if err := c.prepareStructuralCallIdentities(); err != nil {
		t.Fatal(err)
	}
	readers.Wait()
	got, ok := c.cachedStructuralCallIdentity(0)
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("published identity = %x, %v; want %x", got, ok, want)
	}
}
