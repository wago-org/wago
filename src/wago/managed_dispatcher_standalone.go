//go:build wago_standalone

package wago

import "fmt"

var standaloneDispatcherFactory func() *Compiled

func compileManagedVoidDispatcher(*RuntimeConfig) (*Compiled, error) {
	if standaloneDispatcherFactory == nil {
		return nil, fmt.Errorf("standalone managed dispatcher was not linked")
	}
	return standaloneDispatcherFactory(), nil
}
