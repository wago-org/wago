// Package command provides Wago's small, dependency-free command tree.
package command

// Flag declares one option a command accepts.
type Flag struct {
	Name  string
	Short string
	Bool  bool
	Arg   string
	Help  string
}

// Cmd describes either a leaf command with Run or a group with Children.
type Cmd struct {
	Name        string
	Aliases     []string
	Summary     string
	Args        string
	Long        string
	Flags       []Flag
	Knobs       []Flag
	PassThrough bool
	Normalize   func([]string) ([]string, error)
	Run         func(*Ctx)
	Children    []*Cmd
	Automation  Features
}

type Features uint8

const (
	JSONOutput Features = 1 << iota
	DryRun
)

func (c *Cmd) Supports(feature Features) bool { return c.Automation&feature != 0 }

// Child finds a direct subcommand by its canonical name or alias.
func (c *Cmd) Child(name string) *Cmd {
	for _, child := range c.Children {
		if child.Name == name {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == name {
				return child
			}
		}
	}
	return nil
}

// Label removes the leading executable name from a command path.
func (c *Cmd) Label(path string) string {
	const prefix = "wago "
	if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
		return path[len(prefix):]
	}
	return path
}
