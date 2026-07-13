package codegen

import "fmt"

// OutputBufferTooSmallError means an explicitly supplied fixed output buffer
// could not hold the generated code. Callers may retry with normal heap output.
// It is distinct from LimitError: the generated code may still be within the
// configured native-code limit.
type OutputBufferTooSmallError struct {
	Capacity int
	Used     int
}

func (e *OutputBufferTooSmallError) Error() string {
	return fmt.Sprintf("native code output buffer too small: need %d bytes (capacity %d)", e.Used, e.Capacity)
}

// LimitError reports a deterministic backend resource-budget exhaustion.
// Frontends may translate it into their public resource-limit error type.
type LimitError struct {
	Resource string
	Limit    int
	Used     int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s limit exceeded: %d bytes (limit %d)", e.Resource, e.Used, e.Limit)
}
