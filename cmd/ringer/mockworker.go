package main

import (
	"os"

	"github.com/corruptmemory/ringer/internal/mockworker"
)

type mockWorkerCommand struct {
	Args struct {
		Spec string `positional-arg-name:"SPEC"`
	} `positional-args:"yes" required:"yes"`
}

func (c *mockWorkerCommand) Execute(args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	os.Exit(mockworker.Run(c.Args.Spec, wd, os.Stdout, os.Stderr))
	return nil
}

func init() {
	parser.AddCommand("mock-worker",
		"Deterministic offline worker (CI/testing)",
		"Parses MOCK_FILE/MOCK_END/MOCK_FAIL spec grammar and writes files into the cwd.",
		&mockWorkerCommand{})
}
