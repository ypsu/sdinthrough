# ast requirements

- The task here is to build an Abstract Syntax Tree out of the tokens.
- The nodes in the AST hold substrings of the original input.
  The AST doesn't interpret the values either.
- For each node print its attribute struct on a single line as shown below.
- Indent child nodes with a dot-space.
- Interpret the input as a flat struct.

Example input:

```
# A Pre comment.
Field1: Value1
Field2: Value2  # A post comment.
```

Example output:

```
{Value:":{" Children:2}
. {Name:"Field1" Value:"Value1" Line:2 Pre:["# A Pre comment.\n"] Post:"\n"}
. {Name:"Field2" Value:"Value2" Line:3 Post:"# A post comment.\n"}
```

## Node kinds

There are the following kinds of nodes in the tree:

- __Value__: Holds a numeric, identifier, or string token.
- __List__: Starts with `[` or `:[` (for flat lists), holds child nodes, and `]` marks the end for the non-flat lists.
- __Struct__: Starts with `{` or `:{` (for flat structs), holds key-value nodes, and `}` marks the end for the non-flat structs.
  The value must immediately follow the key, there cannot be a newline or comment between the key and its value.
  The names must be identifiers (letter/underscore followed by letters/underscores/digits) and must be unique within the struct.
- __Form__: Starts with a form opener token, can contain both named and nameless values.
  The names can be arbitrary non-string bare tokens and do not need to be unique.

## Node attributes

- __Pre__: token list. The comment and newline tokens before the node's value.
- __Name__: a token. The token that represents the field name part in a struct or a named parameter in a form.
  Relevant only for struct and form node children.
- __Value__: a token. The value of the node.
- __Post__: a token. The comment or newline token that follows the value token.
  If empty then it means that the next token is another key or value token on the same line.
- __Line__: an integer. The line number on which the value was originally defined.
- __Children__: node list: The child nodes.
  Relevant only for non-value nodes.
- __PreClose__: token list. The comment and newline tokens that appear after the children but before the close token.
  Relevant only for non-flat, non-value nodes.
- __PostClose__: a token. The comment or newline token that follows the close token.
  Relevant only for non-flat, non-value nodes.

## Flat lists

Lists can be defined in two ways in a struct:

```
AList: [1 2 3]
```

Or with the name having a + at the end:

```
AList+: 1
AList+: 2
AList+: 3
```

The entries must be contiguous.
They cannot be interspersed with other fields.

## Structs

Similarly structs can be defined with the dot notation.
Instead of

```
A: {
	B: 2
	C: {
		D: 3
	}
	E: [4]
}
```

they can be written like this:

```
A.B:   2
A.C.D: 3
A.E+:  4
```

Again, all fields must be defined contiguously.
They cannot be interspersed with other elements.

Flat lists and structs are just syntactic sugar, and the AST parser must desugar them.
After desugaring, the AST nodes won't have dots and pluses in the names, so this won't conflict with the identifier-only-names rule.
