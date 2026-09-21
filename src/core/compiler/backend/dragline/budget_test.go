package dragline

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/dragline/railmach"
)

func TestBoundedFrameBytesClassifiesExactCapacity(t *testing.T) {
	got, err := boundedFrameBytes("test frame", 4094, 32760)
	if err != nil || got != 32752 {
		t.Fatalf("exact frame = %d, %v", got, err)
	}
	_, err = boundedFrameBytes("test frame", 4095, 32760)
	var limit *ResourceLimitError
	if !errors.As(err, &limit) || limit.Resource != "test frame" || limit.Required != 32768 || limit.Limit != 32760 {
		t.Fatalf("over-budget frame = %#v, %v", limit, err)
	}
}

func TestBoundedFrameBytesRejectsArithmeticOverflow(t *testing.T) {
	_, err := boundedFrameBytes("overflow frame", ^uint64(0), 32760)
	var limit *ResourceLimitError
	if !errors.As(err, &limit) || limit.Required != ^uint64(0) || limit.Limit != 32760 {
		t.Fatalf("overflow frame = %#v, %v", limit, err)
	}
}

func TestClassifyResourceLimitPreservesFunctionFailure(t *testing.T) {
	original := &FunctionError{Function: 7, Stage: "railmach", Err: &railmach.BudgetError{Resource: "instruction operands", Required: 65536, Limit: 65535}}
	err := classifyResourceLimit(original)
	var limit *ResourceLimitError
	var function *FunctionError
	if !errors.As(err, &limit) || !errors.As(err, &function) || limit.Required != 65536 || limit.Limit != 65535 || function.Function != 7 {
		t.Fatalf("classified error = %#v, %v", limit, err)
	}
}
