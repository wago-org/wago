// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package version

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/internal/filelock"
	"github.com/wago-org/wago/internal/wagopaths"
)

// Stable across removal and reinstall. Never put this lock in the removable
// version directory. All lifecycle writers take version then active-state locks.
func versionMutationLock(ctx context.Context, d wagopaths.Dirs, ver string) (*filelock.Lock, error) {
	if err := validateVersionStorageName(ver); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, filepath.Join(d.Config, "version-locks", strings.ToLower(ver)+".lock"))
}

func useInstalledVersion(ctx context.Context, d wagopaths.Dirs, ver string, profile wagopaths.Profile, build wagopaths.Build) (wagopaths.Profile, wagopaths.Build, error) {
	lock, err := versionMutationLock(ctx, d, ver)
	if err != nil {
		return "", "", err
	}
	defer lock.Close()
	_, selectedProfile, selectedBuild, ok := installedRuntime(d, ver, profile, build)
	if !ok {
		return "", "", fmt.Errorf("%s %s/%s is not installed (try: wago version install %s)", ver, profile, build, ver)
	}
	if err := setActiveInstallationVersionLocked(d, ver, selectedProfile, selectedBuild); err != nil {
		return "", "", err
	}
	return selectedProfile, selectedBuild, nil
}
