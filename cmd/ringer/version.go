package main

import "fmt"

const version = "0.1.0-dev"

func Version() string { return "ringer " + version }

type versionCommand struct{}

func (c *versionCommand) Execute(args []string) error {
	fmt.Println(Version())
	return nil
}

func init() {
	parser.AddCommand("version", "Print version", "Print the ringer version string.", &versionCommand{})
}
