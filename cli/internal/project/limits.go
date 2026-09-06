package project

import "github.com/wago-org/wago/internal/jsonstrict"

const (
	maxProjectMetadataBytes = 4 << 20
	maxProjectJournalBytes  = 12 << 20 // two base64-encoded metadata files plus framing
)

var projectJSONLimits = jsonstrict.Limits{MaxDepth: 64, MaxValues: 100000}
