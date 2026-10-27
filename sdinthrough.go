// The sdinthrough command is the user interface for the challenge.
// This main package is a wrapper of the runner package.
// See that for more info.
package main

import (
	"bytes"
	"embed"
	"os"
	"os/signal"

	_ "embed"

	"github.com/mattn/go-isatty"
	"github.com/ypsu/sdinthrough/runner"
	"golang.org/x/term"
)

//go:embed testcases.textar
var testcasesTextar []byte

//go:embed README.md docs/* docs/hints/*
var docfs embed.FS

type crlfWriter struct{}

func (crlfWriter) Write(p []byte) (n int, err error) {
	q := bytes.ReplaceAll(p, []byte("\r"), nil)
	r := bytes.ReplaceAll(q, []byte("\n"), []byte("\r\n"))
	os.Stdout.Write(r)
	return len(p), nil
}

func run() int {
	interrupts := make(chan os.Signal, 4)
	signal.Notify(interrupts, os.Interrupt)
	options := []runner.Option{
		runner.Interrupts(interrupts),
		runner.Testcases(testcasesTextar),
		runner.Args(os.Args[1:]...),
		runner.Docs(docfs),
	}

	// Switch the terminal into raw mode if both stdin and stdout are terminals.
	// Otherwise the manual mode is disabled and the output is left untouched.
	if isatty.IsTerminal(os.Stdout.Fd()) {
		fd := int(os.Stdin.Fd())
		oldState, err := term.MakeRaw(fd)
		if err == nil {
			defer term.Restore(fd, oldState)
			terminalInput := make(chan byte, 8)
			options = append(options, runner.RawInput(terminalInput), runner.Writer(crlfWriter{}))
			go func() {
				p := make([]byte, 8)
				for {
					n, err := os.Stdin.Read(p)
					if err != nil {
						return
					}
					for _, c := range p[:n] {
						if c == 3 {
							// SIGINT case.
							interrupts <- os.Interrupt
						} else {
							terminalInput <- c
						}
					}
				}
			}()
		}
	} else {
		options = append(options, runner.EscapeSequences("", [8]string{}))
	}

	return runner.Run(options...)
}

func main() {
	os.Exit(run())
}
