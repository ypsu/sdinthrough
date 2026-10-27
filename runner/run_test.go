package runner

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ypsu/efftesting/efft"
)

func TestDiff(t *testing.T) {
	efft.Init(t)

	var p runparams
	diff := func(a, b string) string { return string(p.coloredOutput([]byte(a), []byte(b))) }

	efft.Effect(diff("a\n", "a\n")).Equals(`
		  a
		  `)

	efft.Effect(diff("a\n", "b\n")).Equals(`
		~ a
		  `)

	efft.Effect(diff("a\nb\nc\n", "a\nd\nc\n")).Equals(`
		  a
		~ b
		  c
		  `)

	efft.Effect(diff("a\nb\nc\nd\ne\n", "a\nf\nc\ng\ne\n")).Equals(`
		  a
		~ b
		  c
		~ d
		  e
		  `)

	efft.Effect(diff("a\nc\n", "a\nd\nc\n")).Equals(`
		  a
		  c
		  `)

	efft.Effect(diff("a\nb\nc\n", "a\nc\n")).Equals(`
		  a
		~ b
		  c
		  `)

	efft.Effect(diff("a  b\na b", "a b")).Equals(`
		~ a··b  ## only whitespace mismatch!
		  a b`)

	efft.Effect(diff("a b", "a  b\na b")).Equals("~ a·b  ## only whitespace mismatch!")

	efft.Effect(diff("a\nb\nc\nd\ne\nf\n", "a\nc\ne\nf\n")).Equals(`
		  a
		~ b
		~ c
		~ d
		  e
		  f
		  `)

	efft.Effect(diff("a\nc\ne\nf\n", "a\nb\nc\nd\ne\nf\n")).Equals(`
		  a
		~ c
		  e
		  f
		  `)
}

func TestExecute(t *testing.T) {
	efft.Init(t)

	exec := func(cancelTime time.Duration, timeout time.Duration, dir string, stdin string, args ...string) string {
		var interrupts chan os.Signal
		if cancelTime != 0 {
			interrupts = make(chan os.Signal)
			go func() {
				<-time.After(cancelTime)
				interrupts <- os.Interrupt
			}()
		}
		o, e, err := execute(interrupts, timeout, dir, []byte(stdin), args...)
		var r []string
		if len(o) > 0 {
			r = append(r, fmt.Sprintf("stdout=%q", o))
		}
		if len(e) > 0 {
			r = append(r, fmt.Sprintf("stderr=%q", e))
		}
		if err != nil {
			r = append(r, fmt.Sprintf("err=%q", err.Error()))
		}
		return strings.ReplaceAll(strings.ReplaceAll(strings.Join(r, "\n"), "\"", ""), "\\n", "↵")
	}

	efft.Effect(exec(0, time.Second, "", "", "echo", "Hello!")).Equals("stdout=Hello!↵")
	efft.Effect(exec(0, time.Second, "", "", "sh", "-c", "echo Hello; echo World >&2")).Equals(`
		stdout=Hello↵
		stderr=World↵`)
	efft.Effect(exec(0, time.Second, "/", "", "pwd")).Equals("stdout=/↵")
	efft.Effect(exec(0, time.Second, "", "Hello input!", "cat")).Equals("stdout=Hello input!")
	efft.Effect(exec(0, time.Second, "", "", "sh", "-c", "echo Hello; echo World >&2; false")).Equals(`
		stdout=Hello↵
		stderr=World↵
		err=exit status 1`)
	efft.Effect(exec(0, time.Second, "", "", "sh", "-c", "echo Start1; echo Start2 >&2; sleep 0.01; echo Done1; echo Done2 >&2")).Equals(`
		stdout=Start1↵Done1↵
		stderr=Start2↵Done2↵`)
	efft.Effect(exec(0, 20*time.Millisecond, "", "", "sh", "-c", "echo Start1; echo Start2 >&2; sleep 1; echo Done1; echo Done2 >&2")).Equals(`
		stdout=Start1↵
		stderr=Start2↵
		err=sdinthrough.TimedOut timeout=20ms: use a timeout flag to bump this if needed`)
	efft.Effect(exec(20*time.Millisecond, time.Second, "", "", "sh", "-c", "echo Start1; echo Start2 >&2; sleep 1; echo Done1; echo Done2 >&2")).Equals(`
		stdout=Start1↵
		stderr=Start2↵
		err=sdinthrough.Canceled`)
}

func TestLoop(t *testing.T) {
	efft.Init(t)
	testfile := filepath.Join(t.TempDir(), "testfile")

	var testcases = []byte(`
== unescape.Task1
"hello"
== unescape.Task2
"line1\nline2\nline3\n"`)

	w := &bytes.Buffer{}
	writeDone := make(chan bool, 4)
	read := func() []byte {
		<-writeDone
		r := bytes.ReplaceAll(w.Bytes(), []byte("\033"), []byte("\\e"))
		w.Reset()
		return r
	}

	type execresult struct {
		stdout string
		stderr string
		err    error
	}
	execresults := make(chan execresult, 4)
	executor := func(_ <-chan os.Signal, timeout time.Duration, _ string, input []byte, args ...string) ([]byte, []byte, error) {
		fmt.Fprintf(w, "runnertest.Exec timeout=%v args=%q input=%q\n", timeout, args, input)
		r := <-execresults
		return []byte(r.stdout), []byte(r.stderr), r.err
	}
	interrupts := make(chan os.Signal, 4)

	// Initial verdict.
	t.Chdir(filepath.Dir(testfile))
	go Run(
		EscapeSequences("_CLS_", [8]string{"_RESET_", "_RED_", "_GREEN_", "_YELLOW_", "_BLUE_", "_PURPLE_", "_CYAN_", "_SHOULDNOTBEUSED2_"}),
		Writer(w),
		Testcases(testcases),
		Executor(executor),
		Interrupts(interrupts),
		Done(func() { writeDone <- true }),
		Args("-buildcmd=buildcmd buildarg", "-context=1", "runcmd", "runarg"),
	)
	execresults <- execresult{"", "", nil} // Build
	execresults <- execresult{"", "", nil} // Run
	efft.Effect(read()).Equals(`
		Building... runnertest.Exec timeout=10s args=["buildcmd" "buildarg"] input=""
		Running... runnertest.Exec timeout=2s args=["runcmd" "runarg"] input="unescape Task1 data=2268656c6c6f22\nunescape Task2 data=226c696e65315c6e6c696e65325c6e6c696e65335c6e22\n"

		_CLS__YELLOW_== unescape.Task1 (1/2)_RESET_

		-> miss  unescape.Task1 [start]_RESET_
		   miss  unescape.Task2_RESET_

		_YELLOW_== intro_RESET_
		Welcome to the sdinthrough challenge!
		Read https://github.com/ypsu/sdinthrough/blob/main/README.md for instructions.

		_YELLOW_== input_RESET_
		"hello"
		_YELLOW_== want.result_RESET_
		hello
		_YELLOW_== evaluationError_RESET_
		sdinthrough.StderrParseError1: stderr must begin with "==NextNotes==\n", got nothing; did you miss flushing?

		_YELLOW_== verdict_RESET_
		_PURPLE_###########
		## ERROR ##
		###########_RESET_
		`)

	// Task 1.
	execresults <- execresult{"Build OK", "Build warning", nil}
	execresults <- execresult{"==NextAnswer==\nhello", "==NextNotes==\n", nil}
	efft.Must(os.WriteFile(testfile, nil, 0644))
	efft.Effect(read()).Equals(`
		./testfile changed... Building... runnertest.Exec timeout=10s args=["buildcmd" "buildarg"] input=""
		Running... runnertest.Exec timeout=2s args=["runcmd" "runarg"] input="unescape Task1 data=2268656c6c6f22\nunescape Task2 data=226c696e65315c6e6c696e65325c6e6c696e65335c6e22\n"

		_CLS__YELLOW_== unescape.Task1 (1/2)_RESET_

		-> _GREEN_  ok  unescape.Task1 [start]_RESET_
		   miss  unescape.Task2_RESET_

		_YELLOW_== intro_RESET_
		Welcome to the sdinthrough challenge!
		Read https://github.com/ypsu/sdinthrough/blob/main/README.md for instructions.

		_YELLOW_== input_RESET_
		"hello"
		_YELLOW_== want.answer_RESET_
		hello
		_YELLOW_== buildlog_RESET_
		Build OKBuild warning
		_YELLOW_== verdict_RESET_
		_GREEN_########
		## OK ##
		########_RESET_
		`)

	// Build error 1.
	execresults <- execresult{"Build stdout\n", "Build error\n", fmt.Errorf("exitcode 1")}
	efft.Must(os.WriteFile(testfile, nil, 0644))
	efft.Effect(read()).Equals(`
		./testfile changed... Building... runnertest.Exec timeout=10s args=["buildcmd" "buildarg"] input=""

		_CLS__YELLOW_== unescape.Task1 (1/2)_RESET_

		-> _GREEN_  ok  unescape.Task1 [start]_RESET_
		   miss  unescape.Task2_RESET_

		_YELLOW_== intro_RESET_
		Welcome to the sdinthrough challenge!
		Read https://github.com/ypsu/sdinthrough/blob/main/README.md for instructions.

		_YELLOW_== input_RESET_
		"hello"
		_YELLOW_== want.answer_RESET_
		hello
		_YELLOW_== buildlog_RESET_
		Build stdout
		Build error

		_YELLOW_== evaluationError_RESET_
		sdinthrough.Build: exitcode 1

		_YELLOW_== verdict_RESET_
		_PURPLE_###########
		## ERROR ##
		###########_RESET_
		`)

	// Build error 2.
	execresults <- execresult{"Build stdout 2\n", "Build error 2\n", fmt.Errorf("exitcode 2")}
	efft.Must(os.WriteFile(testfile, nil, 0644))
	efft.Effect(read()).Equals(`
		./testfile changed... Building... runnertest.Exec timeout=10s args=["buildcmd" "buildarg"] input=""

		_CLS__YELLOW_== unescape.Task1 (1/2)_RESET_

		-> _GREEN_  ok  unescape.Task1 [start]_RESET_
		   miss  unescape.Task2_RESET_

		_YELLOW_== intro_RESET_
		Welcome to the sdinthrough challenge!
		Read https://github.com/ypsu/sdinthrough/blob/main/README.md for instructions.

		_YELLOW_== input_RESET_
		"hello"
		_YELLOW_== want.answer_RESET_
		hello
		_YELLOW_== buildlog_RESET_
		Build stdout 2
		Build error 2

		_YELLOW_== evaluationError_RESET_
		sdinthrough.Build: exitcode 2

		_YELLOW_== verdict_RESET_
		_PURPLE_###########
		## ERROR ##
		###########_RESET_
		`)

	// Task 2 bad.
	execresults <- execresult{"Build OK", "Build warning", nil}
	execresults <- execresult{"==NextAnswer==\nhello==NextAnswer==\nline1\nline4\nline3\n", "==NextNotes==\n==NextNotes==\nTest note.", nil}
	efft.Must(os.WriteFile(testfile, nil, 0644))
	efft.Effect(read()).Equals(`
		./testfile changed... Building... runnertest.Exec timeout=10s args=["buildcmd" "buildarg"] input=""
		Running... runnertest.Exec timeout=2s args=["runcmd" "runarg"] input="unescape Task1 data=2268656c6c6f22\nunescape Task2 data=226c696e65315c6e6c696e65325c6e6c696e65335c6e22\n"

		_CLS__YELLOW_== unescape.Task2 (2/2) (+1 since startup)_RESET_
		   _GREEN_  ok  unescape.Task1 [start] [best]_RESET_
		-> _RED_fail  unescape.Task2_RESET_


		_YELLOW_== input_RESET_
		"line1\nline2\nline3\n"
		_YELLOW_== your.notes_RESET_
		Test note.
		_YELLOW_== want.answer_RESET_
		_GREEN_  line1
		_RED_~ _GREEN_line_RED_2_GREEN_
		_GREEN_  line3
		_GREEN_  _RESET_
		_YELLOW_== got.answer_RESET_
		_GREEN_  line1
		_RED_~ _GREEN_line_RED_4_GREEN_
		_GREEN_  line3
		_GREEN_  _RESET_
		_YELLOW_== buildlog_RESET_
		Build OKBuild warning
		_YELLOW_== verdict_RESET_
		_CYAN_###########
		## WRONG ##
		###########_RESET_
		Progressed 1 testcases.
		`,
	)

	// Task 2.
	execresults <- execresult{"", "", nil}
	execresults <- execresult{"==NextAnswer==\nhello==NextAnswer==\nline1\nline2\nline3\n", "==NextNotes==\n==NextNotes==\n", nil}
	efft.Must(os.WriteFile(testfile, nil, 0644))
	efft.Effect(read()).Equals(`
		./testfile changed... Building... runnertest.Exec timeout=10s args=["buildcmd" "buildarg"] input=""
		Running... runnertest.Exec timeout=2s args=["runcmd" "runarg"] input="unescape Task1 data=2268656c6c6f22\nunescape Task2 data=226c696e65315c6e6c696e65325c6e6c696e65335c6e22\n"

		_CLS__YELLOW_== unescape.Task2 (2/2) (+1 since startup)_RESET_
		   _GREEN_  ok  unescape.Task1 [start] [best]_RESET_
		-> _GREEN_  ok  unescape.Task2_RESET_


		_YELLOW_== input_RESET_
		"line1\nline2\nline3\n"
		_YELLOW_== want.answer_RESET_
		line1
		line2
		line3

		_YELLOW_== verdict_RESET_
		_GREEN_########
		## OK ##
		########_RESET_
		`,
	)

	// Task 2.
	execresults <- execresult{"", "", nil}
	execresults <- execresult{"==NextAnswer==\nnothello==NextAnswer==\nline1\nline2\nline3\n", "==NextNotes==\n==NextNotes==\n", nil}
	efft.Must(os.WriteFile(testfile, nil, 0644))
	efft.Effect(read()).Equals(`
		./testfile changed... Building... runnertest.Exec timeout=10s args=["buildcmd" "buildarg"] input=""
		Running... runnertest.Exec timeout=2s args=["runcmd" "runarg"] input="unescape Task1 data=2268656c6c6f22\nunescape Task2 data=226c696e65315c6e6c696e65325c6e6c696e65335c6e22\n"

		_CLS__YELLOW_== unescape.Task1 (1/2)_RESET_

		-> _RED_fail  unescape.Task1 [start] [best]_RESET_
		   _GREEN_  ok  unescape.Task2_RESET_

		_YELLOW_== intro_RESET_
		Welcome to the sdinthrough challenge!
		Read https://github.com/ypsu/sdinthrough/blob/main/README.md for instructions.

		_YELLOW_== input_RESET_
		"hello"
		_YELLOW_== want.answer_RESET_
		_RED_~ _GREEN__RED__GREEN_hello_RESET_
		_YELLOW_== got.answer_RESET_
		_RED_~ _GREEN__RED_not_GREEN_hello_RESET_
		_YELLOW_== verdict_RESET_
		_RED_###############
		## REGRESSED ##
		###############_RESET_
		Regressed 1 testcases, best run: 1 solved testcases.
		`)

	// Quitting.
	interrupts <- os.Interrupt
	efft.Effect(read()).Equals(`
		Quitting...
		`)
}
