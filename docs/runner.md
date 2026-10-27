# runner requirements

You run the challenge evaluator (runner from now on) with the following command:

```
go run github.com/ypsu/sdinthrough@latest ${SRC?}
```

where `$SRC` is your source file.
Your source code for all tasks is expected to be in this single file.

The runner supports most common programming languages out of the box.
If yours isn't or you have multi-file source code then run

```
go run github.com/ypsu/sdinthrough@latest -help
```

to see how to pass custom build and run commands.

Every time you save your source code, the runner fully reevaluates your solution.

## Input

The input to your solver is provided on stdin, one line per testcase.
Each line has the following structure:

```
task TestcaseName data=HexEncodedData
```

There are multiple different tasks in this challenge, dispatch your solutions based on the Task part.

Example:

```
unescape SingleWord data=2268656c6c6f22
unescape Newline2 data=226c696e6520315c6e6c696e6520325c6e6c696e6520335c6e22
done Done data=
```

These represent two testcases for the `unescape` challenge and one for the `done` challenge:

- unescape.SingleWord has the input `"hello"`,
- unescape.Newline2 has the input `"line 1\nline 2\nline 3\n"`,
- done.Done has an empty input.

Your solution needs to emit the output for each testcase on both stdout and stderr in the format described next.

## Your stdout

For each testcase output the prefix `==NextAnswer==\n` and the testcase's output.
The output must match exactly the expected output for all non-error testcases (i.e., don't add anything extra, not even a trailing newline).
You must solve the testcases in the order they are given.

For the above input the output should be the following:

```
==NextAnswer==
hello==NextAnswer==
line 1
line 2
line 3
==NextAnswer==
```

## Your stderr

For each testcase output the prefix `==NextNotes==\n` and optionally arbitrary debug info.
This is then also displayed in the evaluation.
Meant to be used for printf debugging but otherwise the runner ignores this data stream.

## Error testcases

Some testcases are meant to fail.
Emit "ERROR\n" for these on stdout followed by an arbitrary error message of your choosing.
For these testcases you don't need to match the expected error message, just the "ERROR\n" prefix.

## Debugging

Encountered a tricky testcase?
Extract that sole testcase via the `-batch=wire:task.Testcase` flag so that you can use it on its own:

```
$ go run github.com/ypsu/sdinthrough@latest -batch=wire:unescape.TwoWords >input
$ yourdebugger yourbinary <input
```

You can also use `-batch=input:task.Testcase` to inspect the testcase input:

```
$ go run github.com/ypsu/sdinthrough@latest -batch=input:unescape.TwoWords
"hello world"
```
