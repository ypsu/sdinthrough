// The stdump effdump contains the solutions and a couple other golden outputs.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing/fstest"
	"time"

	_ "embed"

	"github.com/ypsu/effdump"
	"github.com/ypsu/efftesting/efft"
	"github.com/ypsu/sdinthrough/runner"
	"github.com/ypsu/sdinthrough/solver"
	"github.com/ypsu/textar"
)

//go:embed dump.go
var source string

var testcases = []byte(`These are dummy testcases for testing.
== unescape.Task1
"1\n"
== unescape.Task2
"2\n3"
== unescape.Task3
"4\n5\n6\n"
== unescape.Task4
"\7"
== unescape.Task5
"8\n9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999\n"`)

var spaceMarkerRE = regexp.MustCompile("(?m)( |\t)$")

func markTrailingSpace(p []byte) []byte {
	if !spaceMarkerRE.Match(p) {
		return p
	}
	q := make([]byte, 0, len(p))
	for line := range bytes.Lines(p) {
		spaceStart := len(bytes.TrimRight(line, " \t\n"))
		q = append(q, line[:spaceStart]...)
		for _, c := range line[spaceStart:] {
			if c == ' ' {
				q = append(q, []byte("·")...)
			} else if c == '\t' {
				q = append(q, []byte("≫\t")...)
			} else if c == '\n' {
				q = append(q, '\n')
			}
		}
	}
	return q
}

func dump() (*effdump.Dump, error) {
	os.Setenv("EFFUP", "1")
	os.Setenv("SDINTHROUGH_TESTVAR", "testvalue")
	defer func(orig *time.Location) { time.Local = orig }(time.Local)
	staticTZ, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return nil, fmt.Errorf("stdump.LoadTestTZ: %v", err)
	}
	time.Local = staticTZ

	input, err := os.ReadFile("../testcases.textar")
	if err != nil {
		input, err = os.ReadFile("testcases.textar")
		if err != nil {
			return nil, fmt.Errorf("stdump.ReadTestcases: %v", err)
		}
	}

	dump := effdump.New("stdump")

	// Add the solver testcases.
	inputar := textar.Parse(input)
	stdout, stderr := solver.Solve(solver.EncodeTestcases(inputar.Files))
	answers, notes := bytes.Split(stdout, []byte("==NextAnswer==\n")), bytes.Split(stderr, []byte("==NextNotes==\n"))
	answers, notes = answers[1:], notes[1:]
	if len(inputar.Files) != len(answers) || len(inputar.Files) != len(notes) {
		return nil, fmt.Errorf("stdump.BadResultCount inputs=%d answers=%d notes=%d", len(inputar.Files), len(answers), len(notes))
	}
	displayws := os.Getenv("DISPLAYWS") != ""
	spaceDisplayer := strings.NewReplacer(" ", "·", "\t", "≫\t")
	for i, input := range inputar.Files {
		a := textar.Archive{Comment: markTrailingSpace(input.Data)}
		answer := answers[i]
		if displayws {
			answer = []byte(spaceDisplayer.Replace(string(answer)))
		}
		a.Files = append(a.Files, textar.File{Name: "output", Data: markTrailingSpace(answer)})
		if len(bytes.TrimSpace(notes[i])) > 0 {
			a.Files = append(a.Files, textar.File{Name: "log", Data: notes[i]})
		}
		dump.Add("solution/"+input.Name, a.Format())
	}

	// Add the runner tool testcases.
	w := &bytes.Buffer{}
	testargs := runner.Options(runner.Writer(w), runner.Testcases(testcases), runner.EscapeSequences("", [8]string{}))
	run := func(options ...runner.Option) string {
		w.Reset()
		exitCode := runner.Run(testargs, runner.Options(options...))
		if exitCode != 0 {
			fmt.Fprintf(w, "\nExitCode: %d\n", exitCode)
		}
		return w.String()
	}

	// Create a docfs.
	docfs := fstest.MapFS{
		"README.md":            {Data: []byte("This is the readme.\n")},
		"docs/runner.md":       {Data: []byte("This is the task guidance.\n")},
		"docs/hints/runner.md": {Data: []byte("This is the task hint file.\n")},
	}

	// Flag parsing tests.
	dump.Add("runner/NoArgs", run())
	dump.Add("runner/Help", run(runner.Docs(docfs), runner.Args("-help")))
	dump.Add("runner/BadFlag", run(runner.Args("-badflag")))
	dump.Add("runner/UnsupportedLanguage", run(runner.Args("solution-unknown-lang.ext")))
	dump.Add("runner/ListDoc", run(runner.Docs(docfs), runner.Args("-doc=ls")))
	dump.Add("runner/GoodDoc1", run(runner.Docs(docfs), runner.Args("-doc=runner")))
	dump.Add("runner/GoodDoc2", run(runner.Docs(docfs), runner.Args("-doc=readme")))
	dump.Add("runner/BadDoc", run(runner.Docs(docfs), runner.Args("-doc=boo")))

	// Command invocation testcases.
	execute := func(_ <-chan os.Signal, _ time.Duration, _ string, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
		switch args[0] {
		case "nop":
			return nil, nil, nil
		case "stderrcat":
			return nil, stdin, nil
		case "static":
			return []byte(args[1] + "\n"), []byte(args[2] + "\n"), nil
		case "error":
			return []byte("stdout\n"), []byte("stderr\n"), fmt.Errorf("%s", args[1])
		case "expand":
			return []byte(strings.NewReplacer("n", "\n", "/", "==NextAnswer==\n").Replace(args[1])),
				[]byte(strings.NewReplacer("n", "\n", "/", "==NextNotes==\n").Replace(args[2])),
				nil
		}
		return nil, nil, fmt.Errorf("stdump.UnknownCommand args=%q", args)
	}
	testargs = runner.Options(runner.Writer(w), runner.Testcases(testcases), runner.Executor(execute), runner.EscapeSequences("", [8]string{}))
	dump.Add("runner/DumpTestcase", run(runner.Args("-batch=wire:unescape.Task3")))
	dump.Add("runner/DumpRawTestcase", run(runner.Args("-batch=input:unescape.Task3")))
	dump.Add("runner/LogInput", run(runner.Args("-batch=all", "-buildcmd=nop", "stderrcat")))
	dump.Add("runner/BuildFailed", run(runner.Args("-batch=all", "-buildcmd=error stdump.BuildFailed", "nop")))
	dump.Add("runner/RunFailed", run(runner.Args("-batch=all", "-buildcmd=nop", "error stdump.RunFailed")))
	dump.Add("runner/BuildLogged", run(runner.Args("-batch=all", "-buildcmd=static stdump.StdoutNote stdump.StderrNote", "nop")))

	// Batch tests.
	dump.Add("runner/BadSkip1", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/SKIP/", "//")))
	dump.Add("runner/Skip1", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/SKIPn/", "//")))
	dump.Add("runner/SolveNone", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "nop")))
	dump.Add("runner/SolveNoneWithOutput", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "static", "SomeStdout", "SomeStderr")))
	dump.Add("runner/Solve1", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "expand", "/1n", "/")))
	dump.Add("runner/Solve2", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "expand", "/1n/2n3", "//")))
	dump.Add("runner/Solve3", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n", "///")))
	dump.Add("runner/Solve4", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn", "////")))
	dump.Add("runner/Solve5", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn/8n9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999n", "/////")))
	dump.Add("runner/Solve6", run(runner.Args("-context=2", "-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn/8n9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999n/x", "//////")))
	dump.Add("runner/Solve1WithNote", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n", "/NOTE")))
	dump.Add("runner/SolveWithMoreAnswers", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3n", "/NOTE")))
	dump.Add("runner/SolveWithBadAnswer", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n9n6n/G", "///NOTE/")))
	dump.Add("runner/SolveWithExtraNewlines", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3nn", "//NOTE")))
	dump.Add("runner/SolveWithMissingNewline", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6", "///SomeNote")))
	dump.Add("runner/SolveWithExtraEmptyNote", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n", "//")))
	dump.Add("runner/SolveWithExtraNote", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n", "//NOTE")))
	dump.Add("runner/SolveWithExtraSpace", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/ 1	n", "/")))
	dump.Add("runner/Solve4MissingNewline", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERROR", "////")))
	dump.Add("runner/Solve4BadError", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORx\n", "////")))
	dump.Add("runner/Solve4BadLongError", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERROR012345678901234567890\n", "////")))
	dump.Add("runner/Solve4WithoutError", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/NOERRORn", "////")))
	dump.Add("runner/Solve4WithErrorMessage", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORnMESSAGE", "////")))
	dump.Add("runner/Solve4WithNote", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORnMESSAGE", "////NOTE")))
	dump.Add("runner/Solve5WithMissingNewline", run(runner.Args("-batch=all", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn/8n9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999", "/////")))

	// JSON tests.
	dump.Add("runner/JSONBadSkip1", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/SKIP/", "//")))
	dump.Add("runner/JSONSkip1", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/SKIPn/", "//")))
	dump.Add("runner/JSONSolveNone", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "nop")))
	dump.Add("runner/JSONSolveNoneWithOutput", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "static", "SomeStdout", "SomeStderr")))
	dump.Add("runner/JSONSolve1", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "expand", "/1n", "/")))
	dump.Add("runner/JSONSolve2", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "expand", "/1n/2n3", "//")))
	dump.Add("runner/JSONSolve3", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n", "///")))
	dump.Add("runner/JSONSolve4", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn", "////")))
	dump.Add("runner/JSONSolve5", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn/8n9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999n", "/////")))
	dump.Add("runner/JSONSolve6", run(runner.Args("-context=2", "-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn/8n9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999n/x", "//////")))
	dump.Add("runner/JSONSolve1WithNote", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n", "/NOTE")))
	dump.Add("runner/JSONSolveWithMoreAnswers", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3n", "/NOTE")))
	dump.Add("runner/JSONSolveWithBadAnswer", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n9n6n/G", "///NOTE/")))
	dump.Add("runner/JSONSolveWithExtraNewlines", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3nn", "//NOTE")))
	dump.Add("runner/JSONSolveWithMissingNewline", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6", "///SomeNote")))
	dump.Add("runner/JSONSolveWithExtraEmptyNote", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n", "//")))
	dump.Add("runner/JSONSolveWithExtraNote", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n", "//NOTE")))
	dump.Add("runner/JSONSolveWithExtraSpace", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/ 1	n", "/")))
	dump.Add("runner/JSONSolve4MissingNewline", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERROR", "////")))
	dump.Add("runner/JSONSolve4BadError", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORx\n", "////")))
	dump.Add("runner/JSONSolve4BadLongError", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERROR012345678901234567890\n", "////")))
	dump.Add("runner/JSONSolve4WithoutError", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/NOERRORn", "////")))
	dump.Add("runner/JSONSolve4WithErrorMessage", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORnMESSAGE", "////")))
	dump.Add("runner/JSONSolve4WithNote", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORnMESSAGE", "////NOTE")))
	dump.Add("runner/JSONSolve5WithMissingNewline", run(runner.Args("-batch=json", "-buildcmd=nop", "expand", "/1n/2n3/4n5n6n/ERRORn/8n9999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999", "/////")))

	// Build command tests.
	dump.Add("runner/BuildcmdC", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.c")))
	dump.Add("runner/BuildcmdCC1", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.cc")))
	dump.Add("runner/BuildcmdCC2", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.cpp")))
	dump.Add("runner/BuildcmdGo", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.go")))
	dump.Add("runner/BuildcmdJS", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.js")))
	dump.Add("runner/BuildcmdOdin", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.odin")))
	dump.Add("runner/BuildcmdPython", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.py")))
	dump.Add("runner/BuildcmdRust", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.rs")))
	dump.Add("runner/BuildcmdTypescript", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.ts")))
	dump.Add("runner/BuildcmdUnknown", efft.Stringify(runner.BuildCommand("/tmp/bin", "file.ext")))

	return dump, nil
}

func run(ctx context.Context) error {
	d, err := dump()
	if err != nil {
		return err
	}
	d.Run(ctx)
	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
