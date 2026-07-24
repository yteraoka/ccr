package main

import (
	"fmt"
	"os"

	"github.com/yteraoka/ccr/internal/ccr"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var err error
	switch {
	case len(os.Args) < 2:
		err = ccr.RunPicker(nil)
	case os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help":
		ccr.PrintUsage()
		return
	case os.Args[1] == "-v" || os.Args[1] == "--version" || os.Args[1] == "version":
		fmt.Printf("ccr version %s (commit %s, built at %s)\n", version, commit, date)
		return
	default:
		err = ccr.RunPicker(os.Args[1:])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ccr:", err)
		os.Exit(1)
	}
}
