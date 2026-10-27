// The precommit tool runs checks and tests to maintain a high quality codebase.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

var (
	flagFix   = flag.Bool("fix", false, "If true, applies fixes where an autofix is possible.")
	flagWatch = flag.Bool("watch", false, "If true, auto-reruns the tests when the files change.")
)

type checks struct {
	filenames []string
}

func (cs *checks) FastGoFmt(ctx context.Context) error {
	args := []string{"-d"}
	for _, f := range cs.filenames {
		if filepath.Ext(f) == ".go" {
			args = append(args, f)
		}
	}
	if len(args) <= 1 {
		return nil
	}
	cmd := exec.CommandContext(ctx, "gofmt", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("precommit.ExecGoFmt error: %v; output:\n%s", err, bytes.TrimSpace(out))
	}
	if len(out) > 0 {
		return fmt.Errorf("precommit.UnformattedCode:\n%s", bytes.TrimSpace(out))
	}
	return nil
}

func (cs *checks) FastPrecommitHookInstalled(context.Context) error {
	want := []byte("#!/bin/bash\ngo run ./precommit\n")
	err := func() error {
		got, err := os.ReadFile(".git/hooks/pre-commit")
		if err != nil {
			return fmt.Errorf("precommit.PrecommitHookMissing (run with -fix to fix)")
		}
		if !bytes.Equal(got, want) {
			diffw := &strings.Builder{}
			for line := range bytes.SplitSeq(want, []byte{'\n'}) {
				fmt.Fprintf(diffw, "-%s\n", line)
			}
			for line := range bytes.SplitSeq(got, []byte{'\n'}) {
				fmt.Fprintf(diffw, "+%s\n", line)
			}
			return fmt.Errorf("precommit.InvalidPrecommitHook:\n%s", diffw)
		}
		return nil
	}()
	if err != nil && !*flagFix {
		return err
	}
	if err != nil {
		err := os.WriteFile(".git/hooks/pre-commit", want, 0755)
		if err != nil {
			return fmt.Errorf("precommit.WritePrecommitHook: %v", err)
		}
	}
	return nil
}

func (cs *checks) FastPostcommitHookInstalled(context.Context) error {
	want := []byte("#!/bin/bash\ngo run ./stdump save\n")
	err := func() error {
		got, err := os.ReadFile(".git/hooks/post-commit")
		if err != nil {
			return fmt.Errorf("precommit.PostcommitHookMissing (run with -fix to fix)")
		}
		if bytes.Compare(got, want) != 0 {
			diffw := &strings.Builder{}
			for line := range bytes.SplitSeq(want, []byte{'\n'}) {
				fmt.Fprintf(diffw, "-%s\n", line)
			}
			for line := range bytes.SplitSeq(got, []byte{'\n'}) {
				fmt.Fprintf(diffw, "+%s\n", line)
			}
			return fmt.Errorf("precommit.InvalidPostcommitHook:\n%s", diffw)
		}
		return nil
	}()
	if err != nil && !*flagFix {
		return err
	}
	if err != nil {
		err := os.WriteFile(".git/hooks/post-commit", want, 0755)
		if err != nil {
			return fmt.Errorf("precommit.WritePostcommitHook: %v", err)
		}
	}
	return nil
}

// ThreeX verifies that the source doesn't contain three consecutive x characters.
// I use that for marking sections that should not be committed.
func (cs *checks) FastThreeX(context.Context) error {
	marker := "xx" + "x"
	var badfiles, badlines []string
fileLoop:
	for _, fn := range cs.filenames {
		if filepath.Ext(fn) == ".png" {
			continue
		}
		buf, err := os.ReadFile(fn)
		if err != nil {
			return fmt.Errorf("precommit.ReadFileForThreeX: %v", err)
		}
		if bytes.Contains(buf, []byte(marker)) {
			badfiles = append(badfiles, fn)
		}

		// Collect the marked lines from the files.
		// Skip binary files though.
		for _, ch := range buf {
			if ch < 32 && ch != '\t' && ch != '\n' {
				continue fileLoop
			}
		}
		for line := range strings.SplitSeq(string(buf), "\n") {
			if strings.Contains(line, marker) {
				badlines = append(badlines, line)
			}
		}
	}
	if len(badfiles) > 0 {
		return fmt.Errorf("precommit.FilesWithXXX files=%s:\n%s", strings.Join(badfiles, ","), strings.Join(badlines, "\n"))
	}
	return nil
}

func (cs *checks) SlowGoTestsPassing(ctx context.Context) error {
	errors, err := exec.CommandContext(ctx, "go", "test", "./...").Output()
	if eerr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("precommit.RunGoTest err=%q output:\n%s%s", eerr, eerr.Stderr, errors)
	}
	if err != nil {
		return fmt.Errorf("precommit.RunGoTest: %s", err)
	}
	return nil
}

func (cs *checks) NormalRevive(ctx context.Context) error {
	errors, err := exec.CommandContext(ctx, "revive", "./...").Output()
	if eerr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("precommit.RunRevive err=%q output:\n%s%s", eerr, eerr.Stderr, errors)
	}
	if err != nil {
		return fmt.Errorf("precommit.RunRevive: %s", err)
	}
	if len(errors) > 0 {
		errorLines := bytes.Split(bytes.TrimSpace(errors), []byte("\n"))
		slices.SortFunc(errorLines, bytes.Compare)
		return fmt.Errorf("precommit.GoLint:\n%s", bytes.Join(errorLines, []byte("\n")))
	}
	return nil
}

func removeSpeedNote(name string) (string, bool) {
	if n, ok := strings.CutPrefix(name, "Fast"); ok {
		return n, true
	}
	if n, ok := strings.CutPrefix(name, "Normal"); ok {
		return n, true
	}
	if n, ok := strings.CutPrefix(name, "Slow"); ok {
		return n, true
	}
	return name, false
}

func runchecks(ctx context.Context, w io.Writer) (filenames []string, err error) {
	lsfiles, err := exec.CommandContext(ctx, "git", "ls-files").Output()
	if eerr, ok := err.(*exec.ExitError); ok {
		return nil, fmt.Errorf("precommit.RunGitLS err=%q stderr:\n%s", eerr, eerr.Stderr)
	}
	if err != nil {
		return nil, fmt.Errorf("precommit.RunGitLS: %s", err)
	}

	allok := true
	cs := &checks{filenames: strings.Fields(string(lsfiles))}
	checkstype := reflect.TypeFor[*checks]()
	for i := range checkstype.NumMethod() {
		type wantfunc = func(*checks, context.Context) error
		md := checkstype.Method(i)
		mfn, ok := md.Func.Interface().(wantfunc)
		if !ok {
			fmt.Fprintf(os.Stderr, "precommit.InvalidPrecommitCheckWarning name=%s got=%q want=%q\n", md.Name, md.Type, reflect.TypeFor[wantfunc]())
			continue
		}
		name, ok := removeSpeedNote(md.Name)
		if !ok {
			fmt.Fprintf(os.Stderr, "precommit.InvalidPrecommitPrefix name=%s: the method name should begin with Fast/Normal/Slow\n", md.Name)
		}
		fmt.Printf("precommit.RunningCheck name=%s...", name)
		err := mfn(cs, ctx)
		fmt.Printf("\r\033[K")
		if err != nil {
			allok = false
			lines := strings.Split(err.Error(), "\n")
			for i, line := range lines {
				if i > 26 && len(lines) > 30 {
					fmt.Fprintf(w, "\t... (%d more lines)\n", len(lines)-i)
					break
				}
				if i > 0 {
					fmt.Fprintf(w, "\t%s\n", line)
				} else {
					fmt.Fprintf(w, "%s\n", line)
				}
			}
			fmt.Fprintf(w, "\n")
		}
	}
	if !allok {
		return cs.filenames, fmt.Errorf("precommit.SomeChecksFailed")
	}
	return cs.filenames, nil
}

func run(ctx context.Context) error {
	gitdir, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if eerr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("precommit.RunGitDir err=%q stderr:\n%s", eerr, eerr.Stderr)
	}
	if err != nil {
		return fmt.Errorf("precommit.RunGitDir: %s", err)
	}
	if err := os.Chdir(string(bytes.TrimSpace(gitdir))); err != nil {
		return fmt.Errorf("precommit.Chdir: %v", err)
	}

	var watcher *fsnotify.Watcher
	if *flagWatch {
		watcher, err = fsnotify.NewWatcher()
		if err != nil {
			return fmt.Errorf("precommit.CreateWatcher (fsnotify not supported in your OS?): %v", err)
		}
		defer watcher.Close()
		fmt.Printf("\033[H\033[J")
	}
	filenames, err := runchecks(ctx, os.Stdout)
	if !*flagWatch {
		return err
	}
	if err == nil {
		fmt.Println("OK")
	} else {
		fmt.Println(err)
	}

	for {
		for _, fn := range filenames {
			if err := watcher.Add(filepath.Dir(fn)); err != nil {
				return fmt.Errorf("precommit.AddWatchPath dir=%s: %v", filepath.Dir(fn), err)
			}
		}
		select {
		case <-watcher.Events:
		case err := <-watcher.Errors:
			return fmt.Errorf("precommit.WaitWatchEvent: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		}
		// Wait for the events to settle.
	settleLoop:
		for {
			select {
			case <-watcher.Events:
			case <-time.After(100 * time.Millisecond):
				break settleLoop
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		w := &strings.Builder{}
		filenames, err = runchecks(ctx, w)
		fmt.Printf("\033[H\033[J%s", w)
		if err == nil {
			fmt.Println("OK")
		} else {
			fmt.Println(err)
		}
	}
}

func main() {
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
