# sdinthrough: an interactive coding challenge

- See https://iio.ie/sdinthrough for an introduction to this challenge.
- You have to implement a parser and tooling from scratch for a toy configuration language called `sdin`.
  Read more about it at https://iio.ie/sdin.
- This repository provides increasingly challenging testcases that your solution must pass.
- The runner works locally so you can use any programming language that can read stdin and write to stdout/stderr.
- The runner re-evaluates your solution every time you save.
- You need Go 1.27+ in order to execute the runner.

## Quick start

Write your `solution.c` (or `.go`, `.py`, `.rs`, whatever) and run the runner on it (no need to install anything other than Go and your compiler):

```
go run github.com/ypsu/sdinthrough@latest solution.c
```

Read [docs/runner.md](docs/runner.md) to understand what you need to do and then keep updating your `solution.c` based on the runner feedback.

The challenge should work on most systems but I recommend Linux because it's the only system I have tested this on.
You can access a free Linux VM with your Google account at https://shell.cloud.google.com or with your GitHub account at https://github.com/codespaces.

Use `go install github.com/ypsu/sdinthrough@latest` to install the runner permanently into `$HOME/go/bin`.
Assuming `$HOME/go/bin` is in your `$PATH`, you can use `sdinthrough solution.c` for a faster startup.
However, this way you won't pick up fixes until you re-install the binary.

## Requirements

These documents explain what to implement in each task.
Note that these documents are not complete and only give a rough overview of the requirements.
Some requirements are revealed through the testcases and the challenge is to reverse engineer my intent.
In fact, the harder the task, the less documentation it has.

- [docs/runner.md](docs/runner.md)
- [docs/unescape.md](docs/unescape.md)
- [docs/tokenize.md](docs/tokenize.md)
- [docs/ast.md](docs/ast.md)
- [docs/serialize.md](docs/serialize.md)
- [docs/tojson.md](docs/tojson.md)
- [docs/forms.md](docs/forms.md)
- [docs/schema.md](docs/schema.md)
- [docs/fmt.md](docs/fmt.md)

Note that for simplicity the inputs contain only ASCII characters, there are no Unicode testcases.
All testcases are small: correctness is the focus of this challenge, not efficiency.

## Hints

These documents explain the general structure of the recommended solutions.

- [docs/hints/runner.md](docs/hints/runner.md)
- [docs/hints/unescape.md](docs/hints/unescape.md)
- [docs/hints/tokenize.md](docs/hints/tokenize.md)
- [docs/hints/ast.md](docs/hints/ast.md)
- [docs/hints/serialize.md](docs/hints/serialize.md)
- [docs/hints/tojson.md](docs/hints/tojson.md)
- [docs/hints/schema.md](docs/hints/schema.md)
- [docs/hints/fmt.md](docs/hints/fmt.md)

## Community resources

Have you solved some of the tasks here and written a guide or recorded a video explaining your approach?
I would like to feature a couple here!
Share yours in the [discussions section](https://github.com/ypsu/sdinthrough/discussions/categories/show-and-tell) and I'll curate the best ones below:

- [Official solution](https://github.com/ypsu/sdinthrough/blob/main/solver/solver.go) by @ypsu in Go:
  This is my reference solution, it's what the runner uses.
