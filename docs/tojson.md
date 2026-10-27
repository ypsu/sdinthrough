# tojson requirements

- Convert the AST to a json object.
- Make the output a well indented multiline json.
- Use tabs for indentation.
- Interpret the values.
- Stringify field names to comply with the json spec.

## Numbers

Strip all underscores from the token.
They can be used for digit grouping.
If the resulting token is a valid numeric json token, then emit it as is.
Otherwise, interpret the token according to the following patterns:

- Number bases: 0xdead_BEEF, 0o644, 0b1100 -> 3735928559, 420, 12.
- Decimal multipliers: 2K, 3M -> 2000 and 3000000
- Binary multipliers: 2Ki, 3Mi -> 2048 and 3145728.
- Durations: interpret them as milliseconds.
  The suffix determines the multiplier.
  1m, 1.5h -> 60000, 5400000.
- ISO dates: interpret them as milliseconds since the unix epoch.
  2006-01-02, 2006-01-02.15_04_05 -> 1136160000000, 1136214245000.
  Timestamps are in UTC unless otherwise specified (syntax clarified in the tests).

If the resulting value of this interpretation is not an integer, then format it as a floating-point number rounded to 6 digits after the decimal point.
E.g., 1.234_567s -> 1234.567000.

## Identifiers

- Convert `true` and `false` to the corresponding booleans in json.
- Error out for other identifiers if they are used as a value.
- Stringify them into quoted strings when they appear as fields.

## Strings

Print each string value as a double-quoted string.
Multiquote strings need to be interpreted.

Multiline strings:

- These start with three or more `'`, and end with the same number of quotes.
- The content right after the initial run of quotes until the first newline is called a tag and can be ignored in this task.
- The tag cannot contain newline or |.
- Each content line except empty ones must begin with the same tab/space whitespace run as the closing tag line.
- The content or tag cannot contain the opening/closing quote run; the number of quotes must be strictly larger than the longest run in the tag or content.
- Each line of the content (including the last one) must end with a newline.

Raw strings:

- These start with three or more `'`, followed by a tag (which can be ignored), then `|`, the content, and the same number of quotes.
- The tag cannot contain a newline or `|`.
- The content or tag cannot contain the opening/closing quote run; the number of quotes must be strictly larger than the longest run in the tag or content.
- The difference from multiline strings is that sdin won't ignore the leading whitespace in this one and non-empty content doesn't have to end in a newline.

## Forms

Replace the following forms:

- `stdemo.Env(envvar default:some-string)` looks up an environment variable and replaces the form with its value.
  Optionally takes a `default`-named argument for the default value.
  It returns that if the environment variable doesn't exist.
  It errors out if the envvar doesn't exist and there's no default value.
- `stdemo.Echo(sdinString)` parses the argument as an sdin value and replaces the form with that.
  It errors out if the sdin is invalid.

Also implement `map(key1 value1 key2 value2 ...)` which allows expressing arbitrary strings in json object keys.
Ordinarily sdin struct fields are restricted to identifiers, so this gets around that.

## Directives

Directives are comments starting with `#%` followed by a form.
Individual nodes can be annotated with them to add tool-specific meaning.

In this task, if a value has the `stdemo.SkipJSONEmit()` directive attached to it, omit it from the result.

Example input:

```
A: 1
#% stdemo.SkipJSONEmit()
B: 2
C: 3
```

Example output:

```
{
	"A": 1,
	"C": 3
}
```
