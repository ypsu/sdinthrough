# tokenize requirements

Split the input string into tokens and emit them as a metadata struct list.
For each token, print two lines:

- the full token in an interpreted string with special characters backslash quoted,
- a struct with the line number, length, and starting offset.

Recognize the following token kinds:

- Linebreak: a single instance of a newline.
- Bare: a run of letters, numbers, and non-reserved symbols.
- Name: a bare token immediately followed by a colon.
- String: starts with `'` or `"` and ends at the next matching symbol.
  Do not interpret the string, include the full token along with the quotes as the token.
  More on this below.
- Comment: starts with the `#` symbol and ends with the next newline.
- Block boundary: single instance of `{`, `}`, `[`, `]`, or `)`.
  The kind is the symbol itself.
- Form: a bare token immediately followed by a `(`.
  Note that its closing counterpart, `)`, is treated like the rest of the block boundary tokens.

Ignore all tabs and spaces between the tokens.
The `{`, `}`, `[`, `]`, `(`, `)`, `'`, `"`, `:`, and `#` symbols are reserved; these end the bare token parsing.

Example input:

```
Token1
# Some comment.
Token2
```

Example output:

```
"Token1"
{Line:1 Length:6 Start:0}
"\n"
{Line:1 Length:1 Start:6}
"# Some comment.\n"
{Line:2 Length:16 Start:7}
"Token2"
{Line:3 Length:6 Start:23}
```

## Literal strings

These strings start with a single `'` and end at the next `'`.
The string cannot contain `'` itself.
The string cannot contain a newline either; the opening and closing quote must be on the same line.

## Interpreted strings

These strings start with `"` and end at the next unescaped `"`.
Characters can be escaped with `\`:

- `\n`: newline
- `\t`: tab
- `\"`: the double quote symbol
- `\\`: the backslash symbol
- See https://go.dev/ref/spec#String_literals for the full rules.

This is the type of string the full token fragment should be printed as.

Example:

```
{
	printf("hello world")
}
```

is encoded as `"{\n\tprintf(\"hello world\")\n}\n"`.

It's an error for an interpreted string to contain a raw newline; the opening and closing quote must be on the same line.

## Multiquote strings

These strings start with a run of at least three `'` quotes, followed by a non-quote character, and end with a `'` run of the same or greater length.

Example:

```
func main() {
	// A comment with '''.
	return
}
```

can be encoded like this:

```
	'''''
	func main() {
		// A comment with '''.
		return
	}
	'''''
```

The tokenizer itself doesn't distinguish between multiline and raw strings (explained later in the tojson task).
