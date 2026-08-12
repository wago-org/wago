package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
)

type ConfigRequest struct {
	Context       context.Context
	ID            string
	Config        json.RawMessage
	Global, Local bool
}

func Configure(request ConfigRequest) error {
	global, err := project.MutationGlobal(request.Global, request.Local)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(request.ID)
	if err := project.ValidatePluginID(id); err != nil {
		return err
	}
	if len(request.Config) == 0 || !json.Valid(request.Config) {
		return fmt.Errorf("plugin %s configuration must be exactly one valid JSON value", id)
	}
	src, err := depsSource(global)
	if err != nil {
		return err
	}
	buildDir, err := buildDirFor(global)
	if err != nil {
		return err
	}
	return withPluginMutationLock(pluginContext(request.Context), src, func() error {
		manifest, err := project.Read(src)
		if err != nil {
			return err
		}
		lock, err := project.ReadLock(src)
		if err != nil {
			return err
		}
		entry, ok := lock.Plugins[id]
		if !ok {
			return fmt.Errorf("plugin %q is not in the reviewed lock graph", id)
		}
		entry.Config = append(json.RawMessage(nil), request.Config...)
		lock.Plugins[id] = entry
		if err := project.ValidateLock(lock); err != nil {
			return err
		}
		return stageAndPublishLockedState(src, buildDir, manifest, lock, false)
	})
}
