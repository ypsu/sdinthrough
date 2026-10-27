// Package runner implements the CLI tool's logic.
// Usage information follows.
//
// The sdinthrough tool runs your solution against the challenge.
//
// Usage with source code: go run github.com/ypsu/sdinthrough@latest [flags] [source-file] [args...]
// Usage with binary:      go run github.com/ypsu/sdinthrough@latest [flags] [-buildcmd="..."] [binary] [args...]
//
// By default sdinthrough watches the source file's directory for changes.
// Whenever the directory changes, sdinthrough rebuilds your binary and runs it against the challenges.
// It knows how to build and run single-file C, C++, Go, JavaScript, Odin, Python, Rust, and TypeScript out of the box.
// For them it's enough to list the source file as a positional argument.
// If your language is not supported, then provide a build command and the path to the resulting binary.
//
// Note that all sdinthrough flags must come before the positional arguments.
// All arguments (even the flag-like ones) after your binary are passed to your binary.
//
// Set the NO_COLOR=1 environment variable to disable color output.
//
// Use the -doc flag to print task specific documentation.
// Use -doc=runner and -doc=unescape for guidance for the first task.
// Use -doc=ls to list all the available documentation pages.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "embed"

	"github.com/fsnotify/fsnotify"
	"github.com/ypsu/sdinthrough/solver"
	"github.com/ypsu/textar"
)

//go:embed run.go
var thisSourceCode string

type runparams struct {
	w         io.Writer
	args      []string
	testcases []byte
	execute   ExecuteFunc
	docfs     fs.FS
	hascolors bool
	rawinput  <-chan byte

	// For reading SIGINTs.
	interrupts <-chan os.Signal

	// For testing.
	done func()

	// Internal helpers.
	clearScreen string
	colors      [8]string
}

// Option alters how Run() works.
type Option func(*runparams)

// Args sets the command line flags Run() should assume.
func Args(args ...string) Option {
	return func(p *runparams) {
		p.args = args
	}
}

// Docs configures the available documentation for the -doc argument.
func Docs(fs fs.FS) Option {
	return func(p *runparams) {
		p.docfs = fs
	}
}

// Done is called whenever the main loop body finishes.
func Done(f func()) Option {
	return func(p *runparams) {
		p.done = f
	}
}

// EscapeSequences overrides the default escape sequences for the screen clearing and coloring operations.
func EscapeSequences(clearScreen string, colors [8]string) Option {
	return func(p *runparams) {
		p.clearScreen, p.colors = clearScreen, colors
	}
}

// Executor sets the function with which Run() should execute commands.
func Executor(fn ExecuteFunc) Option {
	return func(p *runparams) {
		p.execute = fn
	}
}

// Interrupts sets the interrupts channel from which Run() reads the user's SIGINT signals.
func Interrupts(ch <-chan os.Signal) Option {
	return func(p *runparams) {
		p.interrupts = ch
	}
}

// Options merges multiple Option values into a single option.
func Options(options ...Option) Option {
	return func(p *runparams) {
		for _, f := range options {
			f(p)
		}
	}
}

// RawInput sets the keyboard input channel.
func RawInput(ch <-chan byte) Option {
	return func(p *runparams) {
		p.rawinput = ch
	}
}

// Testcases sets the challenge input Run() should assume.
func Testcases(tc []byte) Option {
	return func(p *runparams) {
		p.testcases = tc
	}
}

// Writer configures the output Run() should write to.
// The default is os.Stdout.
func Writer(w io.Writer) Option {
	return func(p *runparams) {
		p.w = w
	}
}

// ExecuteFunc is a function that can run child commands.
type ExecuteFunc func(interrupts <-chan os.Signal, timeout time.Duration, dir string, stdin []byte, args ...string) (stdout, stderr []byte, err error)

func execute(interrupts <-chan os.Signal, timeout time.Duration, dir string, stdin []byte, args ...string) (stdout, stderr []byte, err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdoutbuf, stderrbuf bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &stdoutbuf, &stderrbuf
	cmd.Dir = dir
	setupCancel(cmd)

	if err := cmd.Start(); err != nil {
		return stdoutbuf.Bytes(), stderrbuf.Bytes(), fmt.Errorf("sdinthrough.Start: %v", err)
	}

	waitch, userErr := make(chan error), error(nil)
	go func() { waitch <- cmd.Wait() }()
	for {
		select {
		case <-interrupts:
			userErr = fmt.Errorf("sdinthrough.Canceled")
			cancel()
			timeout = time.Hour
		case <-time.After(timeout):
			userErr = fmt.Errorf("sdinthrough.TimedOut timeout=%v: use a timeout flag to bump this if needed", timeout)
			cancel()
			timeout = time.Hour
		case err := <-waitch:
			if userErr != nil {
				err = userErr
			}
			return stdoutbuf.Bytes(), stderrbuf.Bytes(), err
		}
	}
}

var spaceDisplayer = strings.NewReplacer(" ", "·", "\t", "≫\t")

func (p *runparams) coloredOutput(got, want []byte) []byte {
	gotlines, wantlines := bytes.Split(got, []byte("\n")), bytes.Split(want, []byte("\n"))
	n, m := len(gotlines), len(wantlines)
	w := &bytes.Buffer{}
	w.Grow(len(got) + 2*n)

	if n == m {
		// Easy case: same number of lines, so compare each line individually.
		for i := range n {
			gotline, wantline := gotlines[i], wantlines[i]
			if bytes.Equal(gotline, wantline) {
				fmt.Fprintf(w, "%s  %s\n", p.colors[2], gotline)
				continue
			}
			if slices.Equal(strings.Fields(string(gotline)), strings.Fields(string(wantline))) {
				fmt.Fprintf(w, "%s~ %s  ## only whitespace mismatch!\n", p.colors[1], spaceDisplayer.Replace(string(gotline)))
				continue
			}

			// Colorize from the first to the last diffing character.
			s, e, k := 0, 0, 1
			for s < min(len(gotline), len(wantline)) && gotline[s] == wantline[s] {
				a, _ := utf8.DecodeRune(gotline[s:])
				b, _ := utf8.DecodeRune(wantline[s:])
				if a != b {
					break
				}
				s++
			}
			for k <= min(len(gotline)-s, len(wantline)-s) && gotline[len(gotline)-k] == wantline[len(wantline)-k] {
				k++
			}
			e = len(gotline) - k + 1
			for e < len(gotline) && gotline[e]&0b_1100_0000 == 0b_1000_0000 {
				e++ // Skip mid-utf8 chars.
			}
			fmt.Fprintf(w, "%s~ %s%s%s%s%s%s\n", p.colors[1], p.colors[2], gotline[:s], p.colors[1], gotline[s:e], p.colors[2], gotline[e:])
		}
		// The buffer ends with a trailing newline; remove it.
		w.Truncate(w.Len() - 1)
		w.WriteString(p.colors[0])
		return w.Bytes()
	}

	var firstbad, firstrevbad int
	for firstbad < min(n, m) && bytes.Equal(gotlines[firstbad], wantlines[firstbad]) {
		firstbad++
	}
	for firstrevbad < min(n-firstbad, m-firstbad) && bytes.Equal(gotlines[n-firstrevbad-1], wantlines[m-firstrevbad-1]) {
		firstrevbad++
	}
	for i := range firstbad {
		fmt.Fprintf(w, "%s  %s\n", p.colors[2], gotlines[i])
	}
	if firstbad < min(n, m) && slices.Equal(strings.Fields(string(gotlines[firstbad])), strings.Fields(string(wantlines[firstbad]))) {
		fmt.Fprintf(w, "%s~ %s  ## only whitespace mismatch!\n", p.colors[1], spaceDisplayer.Replace(string(gotlines[firstbad])))
		firstbad++
	}
	i := firstbad
	if i < n-firstrevbad && i < n && i < m {
		j := 0
		for j < min(len(gotlines[i]), len(wantlines[i])) && gotlines[i][j] == wantlines[i][j] {
			a, _ := utf8.DecodeRune(gotlines[i][j:])
			b, _ := utf8.DecodeRune(wantlines[i][j:])
			if a != b {
				break
			}
			j++
		}
		fmt.Fprintf(w, "%s~ %s%s%s%s\n", p.colors[1], p.colors[2], gotlines[i][:j], p.colors[1], gotlines[i][j:])
		i++
	}
	for i < n-firstrevbad-1 {
		fmt.Fprintf(w, "%s~ %s\n", p.colors[1], gotlines[i])
		i++
	}
	if i < n-firstrevbad && i < n && i < m {
		k := 1
		gotline, wantline := gotlines[i], wantlines[m-(n-i)]
		for k <= min(len(gotline), len(wantline)) && gotline[len(gotline)-k] == wantline[len(wantline)-k] {
			k++
		}
		j := len(gotline) - k + 1
		for j < len(gotline) && gotline[j]&0b_1100_0000 == 0b_1000_0000 {
			j++ // Skip mid-utf8 chars.
		}
		fmt.Fprintf(w, "%s~ %s%s%s\n", p.colors[1], gotline[:j], p.colors[2], gotline[j:])
		i++
	} else if i < n-firstrevbad {
		fmt.Fprintf(w, "%s~ %s\n", p.colors[1], gotlines[i])
		i++
	}
	for ; i < n; i++ {
		fmt.Fprintf(w, "%s  %s\n", p.colors[2], gotlines[i])
	}
	// The buffer ends with a trailing newline; remove it.
	w.Truncate(w.Len() - 1)
	w.WriteString(p.colors[0])
	return w.Bytes()
}

// BuildCommand returns the build command and run command for the given source file.
// Exported for testability reasons.
func BuildCommand(targetbin string, source string) (buildcmd, runcmd []string, err error) {
	os.Setenv("ASAN_OPTIONS", "color=always "+os.Getenv("ASAN_OPTIONS"))
	os.Setenv("FORCE_COLOR", "1")
	ext := filepath.Ext(source)
	runcmd = []string{targetbin}
	switch ext {
	case ".c":
		buildcmd = []string{"cc", "-fdiagnostics-color=always", "-Wall", "-Wextra", "-Wno-unused", "-std=c23", "-fsanitize=address", "-g", "-o", targetbin, source}
	case ".cc", ".cpp":
		buildcmd = []string{"c++", "-fdiagnostics-color=always", "-Wall", "-Wextra", "-Wno-unused", "-std=c++26", "-fsanitize=address", "-g", "-o", targetbin, source}
	case ".go":
		buildcmd = []string{"go", "build", "-o=" + targetbin, source}
	case ".js":
		buildcmd = []string{"node", "-c", source}
		runcmd = []string{"node", source}
	case ".odin":
		buildcmd = []string{"odin", "build", source, "-file", "-out:" + targetbin}
	case ".py":
		buildcmd = []string{"python3", "-c", fmt.Sprintf("import ast; ast.parse(open(%q).read())", source)}
		runcmd = []string{"python3", source}
	case ".rs":
		buildcmd = []string{"rustc", "--color=always", "-A", "unused", "-o", targetbin, source}
	case ".ts":
		buildcmd = []string{"tsc", "--pretty", "--target", "es2024", "--outFile", targetbin, source}
		runcmd = []string{"node", targetbin}
	default:
		return nil, nil, fmt.Errorf("sdinthrough.UnsupportedLanguage ext=%q: use -buildcmd to specify the build command", ext)
	}
	if len(buildcmd) > 0 && os.Getenv("EFFUP") == "" {
		if _, err := exec.LookPath(buildcmd[0]); err != nil {
			return nil, nil, fmt.Errorf("sdinthrough.CompilerNotFoundInPATH compiler=%s", buildcmd[0])
		}
	}
	return buildcmd, runcmd, err
}

func docname(p string) string {
	p = strings.TrimSuffix(p, ".md")
	p = strings.TrimPrefix(p, "docs/")
	p = strings.ToLower(p)
	return p
}

func (p *runparams) run() error {
	w := p.w

	// Define the flags.
	var (
		flagset          = flag.NewFlagSet("", flag.ContinueOnError)
		flagBuildcmd     = flagset.String("buildcmd", "", "Build command with space separated list of arguments. Use a build script if your command needs multiple steps or spaces in arguments.")
		flagBuildtimeout = flagset.Duration("buildtimeout", 10*time.Second, "Abort the build process after this duration.")
		flagContext      = flagset.Int("context", 4, "Progress context: number of testcases to print before and after the current one in the progress report.")
		flagDoc          = flagset.String("doc", "", "Print a documentation page. Use -doc=ls to list the available documentation pages.")
		flagOutputlimit  = flagset.Int("outputlimit", 20, "Limit output reporting to this amount of kilobytes. Prevents runaway output.")
		flagRuntimeout   = flagset.Duration("runtimeout", 2*time.Second, "Abort stuck test process after this duration. Catches accidental infinite loops and similar logic errors in the solution under test.")
		flagWatchdir     = flagset.String("watchdir", "", "Trigger re-compilation and re-evaluation every time a file in this directory changes. Defaults to the directory of the first source file or the current directory if -buildcmd is used.")

		flagBatch = flagset.String("batch", "", `Don't start the interactive, file-watching UI. ID is either task.TestcaseName or a 1-based index. Supported values:
			-batch=all        Evaluate all testcases, and report the first failure or the final summary.
			-batch=json       Evaluate all testcases, and report the first failure and its context or the final summary in JSON.
			-batch=until:ID   Evaluate the testcases until ID, and report the first failure, or the final summary.
			-batch=input:ID   Print the exact, hex-decoded input of the task.
			-batch=wire:ID    Print the encoded input line of the task.
			`)
	)

	// Parse the flags.
	flagset.SetOutput(io.Discard)
	usage := func() {
		lines, usageLines := strings.SplitAfter(thisSourceCode, "\n")[3:], []string{}
		for i := 0; i < len(lines) && strings.HasPrefix(lines[i], "//"); i++ {
			usageLines = append(usageLines, strings.TrimLeft(strings.TrimPrefix(lines[i], "//"), " \t"))
		}
		io.WriteString(w, strings.Join(usageLines, ""))
		fmt.Fprintf(w, "\nFlags:\n")
		flagset.VisitAll(func(f *flag.Flag) {
			fmt.Fprintf(w, "  -%-13s %s Default: %q\n", f.Name, f.Usage, f.DefValue)
		})

		if bi, ok := debug.ReadBuildInfo(); ok && os.Getenv("SDINTHROUGH_TESTVAR") == "" { // on success and not in effdump mode
			fmt.Fprintf(w, "\nVersion: %s\n", bi.Main.Version)
		}
	}
	flagParseErr := flagset.Parse(p.args)
	if flagParseErr == flag.ErrHelp {
		usage()
		return nil
	}
	if flagParseErr != nil {
		return flagParseErr
	}
	if *flagOutputlimit <= 0 {
		return fmt.Errorf("sdinthrough.OutputLimitTooSmall got=%d: the flag -outputlimit must have a positive value", *flagOutputlimit)
	}
	if *flagContext < 1 || *flagContext > 1000 {
		return fmt.Errorf("sdinthrough.ContextOutOfRange got=%d: the flag -context must be between 1 and 1000", *flagContext)
	}
	if *flagDoc == "" && flagset.NArg() == 0 && (*flagBatch == "" || *flagBatch == "all" || *flagBatch == "json" || strings.HasPrefix(*flagBatch, "until:")) {
		usage()
		return nil
	}

	// Print documentation.
	if *flagDoc != "" {
		if p.docfs == nil {
			return nil
		}
		fn := ""
		fs.WalkDir(p.docfs, ".", func(path string, d fs.DirEntry, _ error) error {
			if d.IsDir() {
				return nil
			}
			if *flagDoc == "ls" {
				fmt.Fprintf(w, "%s\n", docname(path))
			} else if docname(path) == *flagDoc {
				fn = path
			}
			return nil
		})
		if *flagDoc == "ls" {
			return nil
		} else if fn == "" {
			return fmt.Errorf("sdinthrough.DocNotFound page=%s", *flagDoc)
		}
		data, err := fs.ReadFile(p.docfs, fn)
		if err != nil {
			return fmt.Errorf("sdinthrough.ReadDocPage: %v", err)
		}
		fmt.Fprint(w, string(data))
		return nil
	}

	tmpdir := os.TempDir()
	binary := filepath.Join(tmpdir, "sdinthrough."+os.Getenv("USER"), "bin"+executableSuffix())
	if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
		return fmt.Errorf("sdinthrough.MkdirBindir dir=%s: %v", filepath.Dir(binary), err)
	}

	// Split the testcases and encode them into the input format.
	testcases := textar.Parse(bytes.ReplaceAll(p.testcases, []byte("\r"), nil)).Files

	// Parse the batch flag.
	batchMode, batchID := "", 0
	if *flagBatch != "" && *flagBatch != "all" && *flagBatch != "demo" && *flagBatch != "json" {
		mode, arg, ok := strings.Cut(*flagBatch, ":")
		if mode != "until" && mode != "input" && mode != "wire" {
			return fmt.Errorf("sdinthrough.InvalidBatchMode: only all, until, input, wire, and json are supported")
		}
		if !ok {
			return fmt.Errorf("sdinthrough.MissingBatchID mode=%s: this batch mode requires supplying a task ID after a colon", mode)
		}
		batchMode, batchID = mode, func() int {
			if id, _ := strconv.Atoi(arg); id >= 1 && id <= len(testcases) {
				return id - 1
			}
			return slices.IndexFunc(testcases, func(tc textar.File) bool { return tc.Name == arg })
		}()
		if batchID == -1 {
			return fmt.Errorf("sdinthrough.InvalidTaskID id=%q: task not found", arg)
		}
	} else {
		batchMode = *flagBatch
	}
	if batchMode == "input" {
		w.Write(testcases[batchID].Data)
		return nil
	}
	if batchMode == "wire" {
		w.Write(solver.EncodeTestcases(testcases[batchID : batchID+1]))
		return nil
	}
	if batchMode == "json" {
		w = io.Discard
	}
	if batchMode == "until" {
		testcases = testcases[:batchID+1]
	}

	// Compute the expected solutions.
	encodedTestcases := solver.EncodeTestcases(testcases)
	os.Setenv("SDINTHROUGH_TESTVAR", "testvalue")
	solutionStdout, _ := solver.Solve(encodedTestcases)
	solutions := bytes.Split(solutionStdout, []byte("==NextAnswer==\n"))[1:]
	if len(testcases) == 0 {
		return fmt.Errorf("sdinthrough.MissingTestcases")
	}
	if len(solutions) != len(testcases) {
		return fmt.Errorf("sdinthrough.PartialInternalSolution solved=%d testcases=%d", len(solutions), len(testcases))
	}

	// Extract the web demo data.
	if batchMode == "demo" {
		var data []string
		for i, tc := range testcases {
			task, testcase, _ := strings.Cut(tc.Name, ".")
			if task == "ast" {
				break
			}
			data = append(data, task, testcase, string(tc.Data), string(solutions[i]))
		}
		fmt.Fprint(w, strings.Join(data, "===\n"))
		return nil
	}

	// Compute the testing commands.
	var buildcmd, runcmd []string
	if *flagBuildcmd != "" {
		buildcmd, runcmd = strings.Split(*flagBuildcmd, " "), flagset.Args()
		buildcmd = slices.DeleteFunc(buildcmd, func(arg string) bool { return arg == "" })
		if *flagBuildcmd == ":" || *flagBuildcmd == "true" {
			buildcmd = nil
		}
		runcmd = slices.DeleteFunc(runcmd, func(arg string) bool { return arg == "" })
		if *flagWatchdir == "" {
			*flagWatchdir = "."
		}
	} else {
		var err error
		buildcmd, runcmd, err = BuildCommand(binary, flagset.Args()[0])
		if err != nil {
			return fmt.Errorf("sdinthrough.LookupBuildCommand: %v", err)
		}
		runcmd = append(runcmd, flagset.Args()[1:]...)
		if *flagWatchdir == "" {
			*flagWatchdir = filepath.Dir(flagset.Arg(0))
		}
	}

	// Define the state.
	manualMode, showHelp, showSummary, currentTestcase, lastAuto := false, false, true, 0, 0
	type evalresult struct {
		outputs  [][]byte
		notes    [][]byte
		correct  []bool
		buildlog string
		err      error
	}
	lastresult := evalresult{
		outputs: make([][]byte, len(testcases)),
		notes:   make([][]byte, len(testcases)),
		correct: make([]bool, len(testcases)),
	}
	handlekey := func(k byte, eval, quit *bool) {
		if !manualMode && (k == 'j' || k == 'k') {
			manualMode, lastAuto = true, currentTestcase
		}
		switch {
		case k == 'j' && currentTestcase+1 < len(testcases):
			currentTestcase++
		case k == 'k' && currentTestcase > 0:
			currentTestcase--
		case k == 'a':
			manualMode, *eval = false, true
		case k == 'h':
			showHelp = !showHelp
		case k == 's':
			showSummary = !showSummary
		case k == 'r':
			*eval = true
		case k == 'q':
			if manualMode {
				manualMode, *eval = false, true
			} else {
				*quit = true
			}
		}
	}

	// Define the evaluation logic.
	bestRun, initialRun := -1, -1
	eval := func() evalresult {
		r := evalresult{}
		if len(buildcmd) > 0 {
			fmt.Fprintf(w, "Building... ")
			bstdout, bstderr, err := p.execute(p.interrupts, *flagBuildtimeout, "", nil, buildcmd...)
			if len(bstdout) > 0 || len(bstderr) > 0 {
				r.buildlog = fmt.Sprintf("%s%s", bstdout, bstderr)
			}
			if err != nil {
				fmt.Fprintf(w, "\n")
				r.err = fmt.Errorf("sdinthrough.Build: %v", err)
				return r
			}
		}

		fmt.Fprintf(w, "Running... ")
		stdout, stderr, runerr := p.execute(p.interrupts, *flagRuntimeout, "", encodedTestcases, runcmd...)
		if runerr != nil {
			if len(stderr) > 0 && !bytes.HasSuffix(stderr, []byte("\n")) {
				stderr = append(stderr, '\n')
			}
			stderr = fmt.Appendf(stderr, "%v\n", runerr)
		}
		fmt.Fprintf(w, "\n")

		// Check result format.
		stdout = bytes.ReplaceAll(stdout, []byte("\r"), nil)
		if !bytes.HasPrefix(stderr, []byte("==NextNotes==\n")) {
			if len(stderr) == 0 {
				r.err = fmt.Errorf("sdinthrough.StderrParseError1: stderr must begin with \"==NextNotes==\\n\", got nothing; did you miss flushing?")
			} else if len(stderr) > *flagOutputlimit*1000+4000 {
				r.err = fmt.Errorf("sdinthrough.StderrParseError2: stderr must begin with \"==NextNotes==\\n\", got:\n%s...\nsdinthrough.StderrLimit: trimmed the remaining %d KB of stderr per the -outputlimit flag", stderr[:*flagOutputlimit*1000], (len(stderr)-*flagOutputlimit*1000)/1000)
			} else {
				r.err = fmt.Errorf("sdinthrough.StderrParseError3: stderr must begin with \"==NextNotes==\\n\", got:\n%s", stderr)
			}
			return r
		}
		if !bytes.HasPrefix(stdout, []byte("==NextAnswer==\n")) {
			if len(stdout) == 0 {
				r.err = fmt.Errorf("sdinthrough.StdoutParseError1: stdout must begin with \"==NextAnswer==\\n\", got nothing; did you miss flushing?")
			} else if len(stdout) > *flagOutputlimit*1000+4000 {
				r.err = fmt.Errorf("sdinthrough.StdoutParseError2: stdout must begin with \"==NextAnswer==\\n\", got:\n%s...\nsdinthrough.StdoutLimit: trimmed the remaining %d KB of stdout per the -outputlimit flag", stdout[:*flagOutputlimit*1000], (len(stdout)-*flagOutputlimit*1000)/1000)
			} else {
				r.err = fmt.Errorf("sdinthrough.StdoutParseError3: stdout must begin with \"==NextAnswer==\\n\", got:\n%s", stdout)
			}
			return r
		}
		r.outputs, r.notes = bytes.Split(stdout, []byte("==NextAnswer==\n"))[1:], bytes.Split(stderr, []byte("==NextNotes==\n"))[1:]
		if len(r.outputs) != len(r.notes) {
			r.err = fmt.Errorf("sdinthrough.UnevenRecords: stdout=%d stderr=%d (inconsistent number of records emitted; make sure to print ==Next*== markers before you start processing a testcase and flush the output)", len(r.outputs), len(r.notes))
			return r
		}
		if len(r.outputs) > len(testcases) {
			r.err = fmt.Errorf("sdinthrough.TooManyRecords: count=%d testcases=%d", len(r.outputs), len(testcases))
			return r
		}

		// Evaluate all testcases.
		r.outputs = append(r.outputs, make([][]byte, len(testcases)-len(r.outputs))...)
		r.notes = append(r.notes, make([][]byte, len(testcases)-len(r.notes))...)
		r.correct = make([]bool, len(testcases))
		firstIncorrect := len(testcases) - 1
		for tc := range len(testcases) {
			if bytes.HasPrefix(r.outputs[tc], []byte("SKIP\n")) {
				r.correct[tc] = true
				continue
			}
			wanterr, goterr := bytes.HasPrefix(solutions[tc], []byte("ERROR\n")), bytes.HasPrefix(r.outputs[tc], []byte("ERROR\n"))
			r.correct[tc] = wanterr == goterr && (wanterr || bytes.Equal(solutions[tc], r.outputs[tc]))
			if firstIncorrect == len(testcases)-1 && (!r.correct[tc] || tc+1 < len(testcases) && r.outputs[tc+1] == nil) {
				firstIncorrect = tc
			}
		}
		if !manualMode {
			currentTestcase = firstIncorrect
		}
		return r
	}

	if batchMode == "json" {
		jsquote := func(s string) []byte {
			v, _ := json.Marshal(s)
			return v
		}
		jsbytesquote := func(s []byte) []byte { return jsquote(string(s)) }
		r := eval()
		switch {
		case r.err != nil && r.buildlog != "":
			fmt.Fprintf(p.w, `{"Verdict":"Error","Error":%s,"BuildLog":%s}`+"\n", jsquote(r.err.Error()), jsquote(r.buildlog))
			return nil
		case r.err != nil:
			fmt.Fprintf(p.w, `{"Verdict":"Error","Error":%s}`+"\n", jsquote(r.err.Error()))
			return nil
		case currentTestcase == len(testcases)-1 && r.correct[currentTestcase]:
			fmt.Fprintf(p.w, `{"Verdict":"Completed"}`+"\n")
			return nil
		}

		passed := 0
		for _, correct := range r.correct {
			if correct {
				passed++
			}
		}
		if r.correct[currentTestcase] {
			fmt.Fprintf(p.w, `{"Summary":"PartialSuccess","Passed":%d,"Total":%d}`+"\n", passed, len(testcases))
		} else {
			fmt.Fprintf(p.w, `{"Summary":"Wrong","Passed":%d,"Total":%d,"FirstWrong":%d}`+"\n", passed, len(testcases), currentTestcase+1)
		}
		p.colors = [8]string{}
		for tc := max(0, currentTestcase-*flagContext); tc <= min(currentTestcase+*flagContext, len(testcases)-1); tc++ {
			switch {
			case r.outputs[tc] == nil:
				fmt.Fprintf(p.w, `{"Verdict":"Missing","TestcaseID":%d,"TestcaseName":%s,"Input":%s,"Want":%s}`+"\n",
					tc+1, jsquote(testcases[tc].Name), jsbytesquote(testcases[tc].Data), jsbytesquote(solutions[tc]))

			case r.correct[tc]:
				fmt.Fprintf(p.w, `{"Verdict":"OK","TestcaseID":%d,"TestcaseName":%s,"Input":%s,"Notes":%s,"Want":%s}`+"\n",
					tc+1, jsquote(testcases[tc].Name), jsbytesquote(testcases[tc].Data), jsbytesquote(r.notes[tc]), jsbytesquote(solutions[tc]))

			default:
				fmt.Fprintf(p.w, `{"Verdict":"Wrong","TestcaseID":%d,"TestcaseName":%s,"Input":%s,"Notes":%s,"WantDiff":%s,"GotDiff":%s,"Want":%s,"Got":%s}`+"\n",
					tc+1, jsquote(testcases[tc].Name), jsbytesquote(testcases[tc].Data), jsbytesquote(r.notes[tc]),
					jsbytesquote(p.coloredOutput(solutions[tc], r.outputs[tc])),
					jsbytesquote(p.coloredOutput(r.outputs[tc], solutions[tc])),
					jsbytesquote(solutions[tc]), jsbytesquote(r.outputs[tc]))
			}
		}
		return nil
	}

	// Define the verdict reporting function.
	verdictOK := fmt.Sprintf("%s########\n## OK ##\n########%s\n", p.colors[2], p.colors[0])
	verdictError := fmt.Sprintf("%s###########\n## ERROR ##\n###########%s\n", p.colors[5], p.colors[0])
	verdictWrong := fmt.Sprintf("%s###########\n## WRONG ##\n###########%s\n", p.colors[6], p.colors[0])
	verdictRegressed := fmt.Sprintf("%s###############\n## REGRESSED ##\n###############%s\n", p.colors[1], p.colors[0])
	verdictSkipped := "#############\n## SKIPPED ##\n#############\n"
	report := func(r evalresult) {
		rw := bytes.NewBuffer(make([]byte, 0, 4096))
		rw.WriteString(p.clearScreen)
		cached := ""
		if r.err != nil {
			r.outputs, r.notes, r.correct = lastresult.outputs, lastresult.notes, lastresult.correct
			if lastresult.outputs[0] != nil {
				cached = " (cached)"
			}
		}
		lastresult = r
		tc := currentTestcase

		// Compute the progress update.
		progressNote := ""
		if !manualMode && tc > bestRun && r.err == nil {
			if bestRun != -1 {
				progressNote = fmt.Sprintf("Progressed %d testcases.\n", tc-bestRun)
			}
			// bestRun is updated only in auto mode intentionally.
			// In case the user enters manual mode to debug, they can see where they entered it.
			bestRun = tc
		}
		wrong := verdictWrong
		if tc < bestRun {
			wrong = fmt.Sprintf("%sRegressed %d testcases, best run: %d solved testcases.\n", verdictRegressed, bestRun-tc, bestRun)
		}

		// Print the header.
		delta := ""
		if initialRun == -1 {
			initialRun = tc
		}
		if initialRun != tc {
			delta = fmt.Sprintf(" (%+d since startup)", tc-initialRun)
		}
		fmt.Fprintf(rw, "%s== %s (%d/%d)%s%s\n", p.colors[3], testcases[tc].Name, tc+1, len(testcases), delta, p.colors[0])
		if batchMode == "" && p.rawinput != nil {
			if manualMode {
				fmt.Fprintf(rw, "manual mode: shows the verdict for the currently selected testcase only, press h for help\n")
				if showHelp {
					fmt.Fprintf(rw, "controls: j/k: next/prev testcase, r: re-evaluate, q/a: quit manual mode, h: hide help, s: hide/show summary\n")
				}
			} else {
				fmt.Fprintf(rw, "auto mode: shows last emitted or first incorrect result, press h for help\n")
				if showHelp {
					fmt.Fprintf(rw, "controls: j/k: switch to manual mode, r: re-evaluate, q: quit, h: hide help, s: hide/show summary\n")
				}
			}
		}
		if showSummary {
			progressContext := *flagContext
			if tc-progressContext-1 >= 0 {
				incorrect := 0
				for i := range tc - progressContext + 1 {
					if !r.correct[i] {
						incorrect++
					}
				}
				if incorrect == 0 {
					fmt.Fprintf(rw, "   ... (%d testcases)\n", tc-progressContext+1)
				} else {
					fmt.Fprintf(rw, "%s   ... (%d testcases, %d incorrect)%s\n", p.colors[1], tc-progressContext+1, incorrect, p.colors[0])
				}
			} else if tc != progressContext {
				rw.WriteByte('\n')
			}
			for i := -progressContext; i <= progressContext; i++ {
				if i == -progressContext && tc+i != 0 || i == progressContext && tc+i != len(testcases)-1 {
					continue
				}
				if tc+i < 0 || tc+i >= len(solutions) {
					rw.WriteByte('\n')
					continue
				}
				if i == 0 {
					fmt.Fprintf(rw, "-> ")
				} else {
					fmt.Fprintf(rw, "   ")
				}
				o := r.outputs[tc+i]
				switch {
				case o == nil:
					rw.WriteString("miss  ")
				case bytes.HasPrefix(o, []byte("SKIP\n")):
					rw.WriteString("skip  ")
				case r.correct[tc+i]:
					rw.WriteString(p.colors[2])
					rw.WriteString("  ok  ")
				default:
					rw.WriteString(p.colors[1])
					rw.WriteString("fail  ")
				}
				rw.WriteString(testcases[tc+i].Name)
				if batchMode == "" && tc+i == initialRun {
					rw.WriteString(" [start]")
				}
				if batchMode == "" && bestRun > initialRun && tc+i+1 == bestRun {
					rw.WriteString(" [best]")
				}
				if batchMode == "" && manualMode && tc+i == lastAuto {
					rw.WriteString(" [auto]")
				}
				rw.WriteString(p.colors[0])
				rw.WriteByte('\n')
			}
			if tc+progressContext+1 < len(testcases) {
				fmt.Fprintf(rw, "   ... (%d testcases)\n", len(testcases)-tc-progressContext)
			} else if tc+progressContext != len(testcases)-1 {
				rw.WriteByte('\n')
			}
			rw.WriteByte('\n')
		}
		if tc == 0 {
			fmt.Fprintf(rw, "%s== intro%s\n", p.colors[3], p.colors[0])
			rw.WriteString("Welcome to the sdinthrough challenge!\n")
			rw.WriteString("Read https://github.com/ypsu/sdinthrough/blob/main/README.md for instructions.\n")
			rw.WriteString("\n")
		}
		fmt.Fprintf(rw, "%s== input%s\n%s\n", p.colors[3], p.colors[0], testcases[tc].Data)

		// Sanitize the results and print the notes.
		trimmedresult, trimmednote := r.outputs[tc], r.notes[tc]
		if len(trimmedresult) > *flagOutputlimit*1000+4000 {
			trimmedresult = fmt.Appendf(nil, "%s...\nsdinthrough.OutputLimit: trimmed the remaining %d KB of output per the -outputlimit flag\n", trimmedresult[:*flagOutputlimit*1000], (len(trimmedresult)-*flagOutputlimit*1000)/1000)
		}
		if len(trimmednote) > *flagOutputlimit*1000+4000 {
			trimmednote = fmt.Appendf(nil, "%s...\nsdinthrough.NoteLimit: trimmed the remaining %d KB of notes per the -outputlimit flag\n", trimmednote[:*flagOutputlimit*1000], (len(trimmednote)-*flagOutputlimit*1000)/1000)
		}
		if len(trimmednote) > 0 {
			fmt.Fprintf(rw, "%s== your.notes%s%s\n%s\n", p.colors[3], cached, p.colors[0], trimmednote)
		}

		// Print the validation error if any.
		verdict := verdictOK
		goterr, wanterr := bytes.HasPrefix(r.outputs[tc], []byte("ERROR\n")), bytes.HasPrefix(solutions[tc], []byte("ERROR\n"))
		hasInvalidByte := false
		for _, c := range r.outputs[tc] {
			if c != '\n' && c != '\t' && c < 32 || c >= 128 {
				hasInvalidByte = true
				break
			}
		}
		switch {
		case r.outputs[tc] == nil:
			fmt.Fprintf(rw, "%s== want.result%s\n%s\n", p.colors[3], p.colors[0], solutions[tc])
			if tc > 0 || r.err == nil {
				fmt.Fprintf(rw, "%s== validationError%s%s\nsdinthrough.MissingOutput: your solution emitted no output for this testcase\n\n", p.colors[3], cached, p.colors[0])
			}
			verdict = wrong
		case bytes.HasPrefix(r.outputs[tc], []byte("SKIP\n")):
			fmt.Fprintf(rw, "%s== want.result%s\n%s\n", p.colors[3], p.colors[0], solutions[tc])
			verdict = verdictSkipped
		case bytes.HasPrefix(r.outputs[tc], []byte("SKIP")):
			fmt.Fprintf(rw, "%s== validationError%s%s\n", p.colors[3], cached, p.colors[0])
			rw.WriteString("sdinthrough.SkipWithoutNewline: need a newline after \"SKIP\"\n\n")
			verdict = wrong
		case wanterr:
			fmt.Fprintf(rw, "%s== want.result%s\n%s\n", p.colors[3], p.colors[0], solutions[tc])
			fmt.Fprintf(rw, "%s== got.result%s%s\n%s\n", p.colors[3], cached, p.colors[0], trimmedresult)
			if goterr {
				// All OK.
				break
			}
			if bytes.HasPrefix(r.outputs[tc], []byte("ERROR")) {
				fmt.Fprintf(rw, "%s== validationError%s%s\n", p.colors[3], cached, p.colors[0])
				q := ""
				if len(r.outputs[tc]) > 16 {
					q = fmt.Sprintf("%q...", r.outputs[tc][:12])
				} else {
					q = fmt.Sprintf("%q", r.outputs[tc])
				}
				fmt.Fprintf(rw, "sdinthrough.InvalidErrorForm got=%s want=%q: need a newline after ERROR\n\n", q, "ERROR\n")
				verdict = wrong
			} else {
				fmt.Fprintf(rw, "%s== validationError%s\n", p.colors[3], p.colors[0])
				rw.WriteString("sdinthrough.NotError: your output must start with an \"ERROR\" line then arbitrary string\n\n")
				verdict = wrong
			}
		case !bytes.Equal(solutions[tc], r.outputs[tc]) && bytes.Equal(bytes.TrimRight(r.outputs[tc], " \t\n"), bytes.TrimRight(solutions[tc], " \t\n")):
			got, want, same, dots := r.outputs[tc], solutions[tc], 0, ""
			for same < min(len(r.outputs[tc]), len(solutions[tc])) && got[same] == want[same] {
				same++
			}
			if same > 32 {
				got, want, dots = got[same-24:], want[same-24:], "..."
			}
			fmt.Fprintf(rw, "%s== want.answer%s\n%s\n", p.colors[3], p.colors[0], solutions[tc])
			fmt.Fprintf(rw, "%s== validationError%s%s\nsdinthrough.MismatchingWhitespace: your output doesn't have the same whitespace at the end\n", p.colors[3], cached, p.colors[0])
			fmt.Fprintf(rw, "want: %s%q\ngot:  %s%q\n\n", dots, want, dots, got)
			verdict = wrong
		case hasInvalidByte:
			fmt.Fprintf(rw, "%s== want.answer%s\n%s\n", p.colors[3], p.colors[0], p.coloredOutput(solutions[tc], trimmedresult))
			fmt.Fprintf(rw, "%s== validationError%s\n", p.colors[3], p.colors[0])
			fmt.Fprintf(rw, "sdinthrough.InvalidCharacter: your output contains a non-printable or non-ASCII character:\n%q\n", trimmedresult)
			verdict = wrong
		case !bytes.Equal(solutions[tc], r.outputs[tc]):
			fmt.Fprintf(rw, "%s== want.answer%s\n%s\n", p.colors[3], p.colors[0], p.coloredOutput(solutions[tc], trimmedresult))
			fmt.Fprintf(rw, "%s== got.answer%s%s\n%s\n", p.colors[3], cached, p.colors[0], p.coloredOutput(trimmedresult, solutions[tc]))
			verdict = wrong
		default:
			// All OK
			fmt.Fprintf(rw, "%s== want.answer%s\n%s\n", p.colors[3], p.colors[0], solutions[tc])
		}

		// Print the evaluation error if there's one.
		if r.buildlog != "" {
			fmt.Fprintf(rw, "%s== buildlog%s\n%s\n", p.colors[3], p.colors[0], r.buildlog)
		}
		if r.err != nil {
			fmt.Fprintf(rw, "%s== evaluationError%s\n%v\n\n", p.colors[3], p.colors[0], r.err)
			progressNote = ""
			verdict = verdictError
		}

		// Print the verdict.
		fmt.Fprintf(rw, "%s== verdict%s\n%s%s", p.colors[3], p.colors[0], verdict, progressNote)
		w.Write(rw.Bytes())
	}

	if batchMode != "" {
		report(eval())
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("sdinthrough.NewFSWatcher: %v", err)
	}
	defer watcher.Close()

	// Add all directories to the watcher except the hidden ones.
	if err := filepath.WalkDir(*flagWatchdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || len(d.Name()) >= 2 && strings.HasPrefix(d.Name(), ".") && path != *flagWatchdir {
			if d.IsDir() {
				return fs.SkipDir
			} else {
				return nil
			}
		}
		if err := watcher.Add(path); err != nil {
			return fmt.Errorf("sdinthrough.AddWatch path=%q: %v", path, err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("sdinthrough.WalkDir: %v", err)
	}

	for {
		report(eval())

		// Don't trigger re-execution if the build produced a binary in the watched directory or something.
		// Eat up all fs events to skip such events.
	ignoreLoop:
		for {
			select {
			case <-watcher.Events:
			case <-time.After(100 * time.Millisecond):
				// Need the timeout here.
				// The file might have been created just before eval() returned but the watcher didn't see it yet.
				break ignoreLoop
			}
		}

		if p.done != nil {
			p.done()
		}

		// Block until the filesystem is modified or a key is pressed.
		var breakTime <-chan time.Time
		triggered := false
	fswaitLoop:
		for {
			select {
			case ev := <-watcher.Events:
				if !triggered {
					fmt.Fprintf(w, "%s changed... ", ev.Name)
					triggered = true
				}
				breakTime = time.After(100 * time.Millisecond)
			case err := <-watcher.Errors:
				return fmt.Errorf("sdinthrough.WaitFSEvent: %v", err)
			case <-breakTime:
				break fswaitLoop
			case k := <-p.rawinput:
				var shouldEval, shouldQuit bool
				handlekey(k, &shouldEval, &shouldQuit)
				for len(p.rawinput) > 0 {
					handlekey(<-p.rawinput, &shouldEval, &shouldQuit)
				}
				if shouldQuit {
					fmt.Fprintln(w, "Quitting...")
					return nil
				}
				if shouldEval {
					break fswaitLoop
				}
				report(lastresult)
			case <-p.interrupts:
				fmt.Fprintln(w, "Quitting...")
				if p.done != nil {
					p.done()
				}
				return nil
			}
		}
	}
}

// Run runs sdinthrough with the given parameters.
// Returns an exitcode.
func Run(options ...Option) int {
	p := &runparams{
		w:           os.Stdout,
		execute:     execute,
		hascolors:   os.Getenv("NO_COLOR") == "",
		clearScreen: "\033[H\033[2J\033[3J",
	}
	if p.hascolors {
		p.colors[0] = "\033[0m"
		for i := 1; i < len(p.colors); i++ {
			p.colors[i] = fmt.Sprintf("\033[3%dm", i)
		}
	}
	for _, option := range options {
		option(p)
	}

	if err := p.run(); err != nil {
		fmt.Fprintf(p.w, "Error: %v\n", err)
		return 1
	}
	return 0
}
