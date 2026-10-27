# tokenize hints

You need a `tokenize(string) -> list:string` function for this.
In it the general structure will be like this:

```
tokens = []
i = 0
for i < len(s) {
    if s[i] == '\'' {
        // Handle literal strings.
        j = i+1
        // Advance j to the next ' character.
        for s[j] != '\'' {
            j++
        }
        tokens.append(s[i:j+1])
        i = j+1
    } else if s[i] == '"' {
        // Handle double quoted strings.
        j = i+1
        // Advance j to the next unquoted " character.
        ...
        tokens.append(s[i:j+1])
        i = j+1
    } else if // and so on.
}
return tokens
```

Note the double loop structure: once you know what token you are parsing, it's easy to write a loop to detect the end.
This leads to a simpler structure than a pure state-machine-based one because the logic is straightforward rather than inverted.
