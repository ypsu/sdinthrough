# serialize requirements

Rewrite the input sdin's AST into a compact one-line representation without any comments.
Only add spaces to separate adjacent values, and print each string value as a double-quoted string.

Sample input:

```
# Remove comments such as this.
Hello: World
List: [
  1
  2  # Remove this comment too.
  '3'
]
```

Sample output:

```
Hello:World List:[1 2 "3"]
```
