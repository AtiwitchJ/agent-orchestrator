package main

import (
	"fmt"
	"os"

	"github.com/modernagent/modern-agent/backend/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitCode(err))
	}
}
