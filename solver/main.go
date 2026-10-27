//go:build exclude

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/ypsu/sdinthrough/solver"
)

func run() error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("solver.ReadInput: %v", err)
	}
	stdout, stderr := solver.Solve(input)
	if i := bytes.IndexByte(stdout, '='); i != -1 {
		// Drop the comment.
		stdout = stdout[i:]
	}
	os.Stdout.Write(stdout)
	os.Stderr.Write(stderr)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
