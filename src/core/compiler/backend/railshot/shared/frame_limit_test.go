package shared

import "testing"

func TestNativeFrameFitsStackFenceIncludesEntryOverhead(t *testing.T) {
	maxBody := MaxNativeFrameBytes - MaxNativeInboundCallBytes
	if !NativeFrameFitsStackFence(maxBody, MaxNativeInboundCallBytes) {
		t.Fatalf("frame at body limit was rejected")
	}
	if NativeFrameFitsStackFence(maxBody+1, MaxNativeInboundCallBytes) {
		t.Fatalf("frame exceeding body limit was accepted")
	}
	if NativeFrameFitsStackFence(0, MaxNativeFrameBytes+1) {
		t.Fatalf("oversized entry overhead was accepted")
	}
}
