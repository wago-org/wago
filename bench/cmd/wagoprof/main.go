// Command wagoprof is the standalone profiling workflow.
package main

import (
	"fmt"
	"github.com/wago-org/wago/internal/profilecmd"
	"os"
)

func main() {
	if err := profilecmd.Run(os.Args[1:], nil); err != nil {
		fmt.Fprintln(os.Stderr, "wagoprof:", err)
		os.Exit(1)
	}
}
