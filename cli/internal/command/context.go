package command

import (
	"strings"

	"github.com/wago-org/wago/cli/internal/ui"
)

// Ctx is a parsed invocation handed to a leaf command.
type Ctx struct {
	Cmd   *Cmd
	Path  string
	Args  []string
	strs  map[string]string
	bools map[string]bool
}

// NewContext constructs a context for focused command tests.
func NewContext(args []string, strs map[string]string, bools map[string]bool) *Ctx {
	if strs == nil {
		strs = map[string]string{}
	}
	if bools == nil {
		bools = map[string]bool{}
	}
	return &Ctx{Args: args, strs: strs, bools: bools}
}

func (c *Ctx) Str(name string) string { return c.strs[name] }
func (c *Ctx) Bool(name string) bool  { return c.bools[name] }

func (c *Ctx) One(what string) string {
	if len(c.Args) != 1 {
		ui.Usage("%s: need exactly one %s", strings.TrimPrefix(c.Path, "wago "), what)
	}
	return c.Args[0]
}

func (c *Ctx) Optional(what string) string {
	switch len(c.Args) {
	case 0:
		return ""
	case 1:
		return c.Args[0]
	default:
		ui.Usage("%s: accepts at most one %s", strings.TrimPrefix(c.Path, "wago "), what)
		return ""
	}
}
