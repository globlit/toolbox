// This is an operator build command, not an agent-facing gateway operation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"toolbox/internal/provision"
)

func main() {
	path := flag.String("config", "", "absolute path to the approved operator JSON")
	out := flag.String("out", "", "absolute output path for the provisioned gateway")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	if err := provision.Build(context.Background(), ".", *path, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Built gateway with pinned configuration and kubectl digests.")
}
