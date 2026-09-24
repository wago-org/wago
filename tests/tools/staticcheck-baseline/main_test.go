package main

import (
	"strings"
	"testing"
)

func TestParseFindingsNormalizesPathsAndLines(t *testing.T) {
	const root = "/checkout"
	input := `{"code":"U1000","location":{"file":"/checkout/src/example.go","line":19,"column":4},"message":"func f is unused"}` + "\n"
	got, err := parseFindings(strings.NewReader(input), root, "standard")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "src/example.go" || got[0].Line != 19 || got[0].Profile != "standard" {
		t.Fatalf("parsed findings = %+v", got)
	}
}

func TestFindingsBeyondBaselineRejectNewMessagesAndDuplicates(t *testing.T) {
	baseline := []finding{{Profile: "standard", File: "src/example.go", Code: "U1000", Message: "func f is unused"}}
	actual := []finding{
		{Profile: "standard", File: "src/example.go", Code: "U1000", Message: "func f is unused", Line: 10},
		{Profile: "standard", File: "src/example.go", Code: "U1000", Message: "func f is unused", Line: 20},
		{Profile: "standard", File: "src/example.go", Code: "SA4006", Message: "value is never used", Line: 30},
	}
	got := findingsBeyondBaseline(actual, baseline)
	if len(got) != 2 || got[0].Line != 20 || got[1].Line != 30 {
		t.Fatalf("new findings = %+v", got)
	}
}
