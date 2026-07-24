package main

import (
	"fmt"
	"os"
)

func main() {
	var err error
	switch {
	case len(os.Args) < 2:
		err = runPicker(nil)
	case os.Args[1] == "list":
		err = runList(os.Args[2:])
	case os.Args[1] == "info":
		err = runInfo(os.Args[2:])
	case os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help":
		printUsage()
		return
	default:
		err = runPicker(os.Args[1:])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ccr:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage:
  ccr [-g]            interactive session picker (current project, or -g for every project)
  ccr list [--timestamps]
  ccr info <session_id>`)
}
