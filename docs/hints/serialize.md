# serialize hints

- Traverse the AST, print the name token, the value token, the children, and the closing token.
- Emit space iff `!lastTokenKind.IsNameToken() && !(lastTokenKind.IsOpeningToken() || currentTokenKind.IsClosingToken())`.
