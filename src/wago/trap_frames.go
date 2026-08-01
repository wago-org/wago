package wago

import (
	"errors"
	"sort"

	wruntime "github.com/wago-org/wago/src/core/runtime"
)

func (in *Instance) decorateTrap(err error) error {
	if err == nil || in == nil || in.c == nil {
		return err
	}
	var trap *wruntime.TrapError
	if !errors.As(err, &trap) {
		return err
	}
	for index := range trap.Frames {
		if trap.Frames[index].FunctionName == "" {
			trap.Frames[index].FunctionName = in.c.functionDisplayName(trap.Frames[index].FunctionIndex)
		}
	}
	return err
}

func (c *Compiled) functionDisplayName(index uint32) string {
	if c.Names != nil {
		for _, association := range c.Names.FunctionNames {
			if association.Index == index {
				return association.Name
			}
		}
	}
	var exports []string
	for name, function := range c.Exports {
		if function >= 0 && uint32(function) == index {
			exports = append(exports, name)
		}
	}
	if len(exports) != 0 {
		sort.Strings(exports)
		return exports[0]
	}
	return ""
}
