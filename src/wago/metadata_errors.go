package wago

import "fmt"

// Share contextual error formatting so compiler and loader validation exits do
// not each carry their own variadic formatting setup. This only runs on errors.
//
//go:noinline
func wrapContextError(context string, err error) error {
	return fmt.Errorf("%s: %w", context, err)
}
