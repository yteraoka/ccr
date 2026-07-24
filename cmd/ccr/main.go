package main

import (
	"fmt"
	"os"

	"github.com/yteraoka/ccr/internal/ccr"
)

func main() {
	var err error
	switch {
	case len(os.Args) < 2:
		err = ccr.RunPicker(nil)
	case os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help":
		ccr.PrintUsage()
		return
	default:
		err = ccr.RunPicker(os.Args[1:])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ccr:", err)
		os.Exit(1)
	}
}
