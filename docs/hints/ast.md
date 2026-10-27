# ast hints

Create the Kind helper class.
It's a class that wraps a token and gives it useful boolean query functions without polluting the Token class.
Create boolean methods on it like Identifier(), Scalar(), Opening(), List(), Flat(), IdentifierPath(), etc.
This is going to help categorize the tokens because you can write this: `if !Kind(token).Identifier() { return error("not identifier!") }`.

Create the Node struct with the fields listed in the task.

Create a recursive block parser function.
It receives a node and the token queue as parameter.
It pops tokens until the first closing token and appends them as child values to the input param.
It also recurses if it sees a block opening token.

Create the flat structs with a loop since the parser can't recurse for them.
