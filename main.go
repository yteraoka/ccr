package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "list":
		err = runList(os.Args[2:])
	case "info":
		err = runInfo(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "cc-resume:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage:
  cc-resume list [--timestamps]
  cc-resume info <session_id>`)
}
