# unescape requirements

This is a short warmup exercise before diving into tokenization.
The input is a string enclosed in double quotes with some characters backslash escaped.
Unescape them and print the string's raw value on stdout.
The spec follows [Go's string literals](https://go.dev/ref/spec#String_literals).
The input will contain only the following escape sequences:

- `\\` -> `\`
- `\"` -> `"`
- `\t` -> a tab
- `\n` -> a newline
- `\OOO` -> single byte represented by the three-digit octal number *OOO*
- `\xHH` -> single byte represented by the two-digit hex number *HH*

Error out on invalid strings not supported by Go's spec.

Example input:

```
"Hello \"world\"!"
```

Example output:

```
Hello "world"!
```
