# runner hints

- In Go you can read the input via a `fmt.Scan(&task, &testcase, &data)` loop.
- In Go you can decode the input data via `hex.DecodeString(strings.TrimPrefix(data, "data="))`.
- Start each iteration of the input loop by writing "==NextAnswer==" and "==NextNotes==" to stdout and stderr.
