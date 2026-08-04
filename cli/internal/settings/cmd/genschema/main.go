package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/wago-org/wago/cli/internal/settings"
)

func main() {
	schema := flag.String("schema", "schema.json", "path to the Wago manifest schema")
	flag.Parse()
	source, err := os.ReadFile(*schema)
	if err != nil {
		fatal(err)
	}
	generated, err := settings.GenerateSchema(source)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*schema, generated, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "genschema:", err)
	os.Exit(1)
}
