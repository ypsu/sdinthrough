# fmt hints

I recommend the following general structure:

- Do a pass over the AST to mark which nodes are single-line.
- Do another pass over the AST to inject newlines where they are missing.
  I.e. if a multiline object's value doesn't have a newline posttoken, then add it.
- In the final pass generate the raw tokens into an array.
  This array doesn't contain any indentation or spaces yet.
- Use a simple loop: grab a line's worth of tokens from the token array and print it formatted.
  Add spaces and indentation where needed.
- For the alignment logic: split the array into alignment runs according to rules in the requirements.
  Compute the column widths in each alignment run and format the runs independently.
- Implement sorting and sdin string formatting with more prepasses over the AST.
