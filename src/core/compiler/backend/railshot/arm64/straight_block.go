//go:build arm64

package arm64

var straightLineBlockEnabled = true

// Ordinary blocks have no header branch site (-1). This distinct sentinel
// records the proven fallthrough-only frame without consuming packed flag bits.
const straightLineBlockSite = -2
