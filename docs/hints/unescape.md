# unescape hints

The recommended code structure is a for loop with a big switch statement:

```
i, n, p, answer := 1, len(input), input, ""
for i < n {
  switch {
  case p[i] != '\\':
    answer += p[i]
    i++
  case i+1 < n && p[i+1] == 'n':
    answer += '\n'
    i += 2
  case i+3 < n && p[i+1] == 'x' && ishex(p[i+2]) && ishex(p[i+3]):
    answer += ...
    i += 4
  case ...
  ...
  }
}
```
