# fmt requirements

Take an sdin object, format it, and print it back.

- If a struct or a map() form was fully defined on a single line, then keep it on a single line.
  Otherwise structs have at most one field-value pair per line.
- Don't inject or remove newlines from lists and other forms.
- Indent with tabs.
- The number of open blocks determines the indentation level in most cases.
- Visually align the second column and the side comment in a run of lines if all lines within it have exactly two same-level non-comment tokens (the first one must be a name, bare, or a string token).
  A run is a contiguous sequence of lines terminated by a blank line, a full line comment, a multiline string, a change of indentation, or a multiline block opener/closer.
- Visually align the comments within a run, with two spaces after the longest line's value.
- Remove unnecessary indentation in objects that don't use the smaller indentation at all.
- Format sdin-tagged strings as sdin.
- Sort child nodes if requested with the `#% sdin.KeepSorted()` directive.

Example input:

```
Struct: {
  Field1: apple # comment 1
F2: banana # comment 2
A:[1 2] B:{X:Y} # comment 3
}
List: [
val1 val2 val3  # comment 1
   v1 v2 v3  # comment 2
 value1 v2    val3

col1 col2     # comment 1
  c1 c2       # comment 2
 column1 column2
]

# Avoid unnecessary indentation like in List2 here:
List1: [
form(
A:1 2
)
]
List2: [form(
A:1 2
)]
```

Example output:

```
Struct: {
	Field1: apple   # comment 1
	F2:     banana  # comment 2
	A:      [1 2]
	B:      {X:Y}   # comment 3
}
List: [
	val1 val2 val3  # comment 1
	v1 v2 v3        # comment 2
	value1 v2 val3

	col1    col2     # comment 1
	c1      c2       # comment 2
	column1 column2
]

# Avoid unnecessary indentation like in List2 here:
List1: [
	form(
		A:1 2
	)
]
List2: [form(
	A:1 2
)]
```

The resulting `Struct`'s four inner lines belong to the same run and have two columns, so align the second column.
When counting tokens for Struct.A and Struct.B, count only `[` and `{` because the remaining ones are on a deeper level.

In `List:`, the first inner run is not composed of two-token lines, so don't align the run's second column.
But do align the comments.

Warning: This task has nightmare difficulty because the exact rules are revealed through the testcases.
The tests are essentially the spec.
This is test-driven development on steroids.
The schema task was a cakewalk compared to this.
Think of it as bonus content for hardcore folks, rather than being part of the challenge.
