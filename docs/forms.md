# forms requirements

This task is demoing sdin's forms feature.
The input is a single form value which should result in an output as described below.

## stdemo.Print()

This form has exactly one string parameter.
Interpret it, and print it.

If the string is tagged with `stdemo.RemoveComments`, remove empty lines and all lines starting with a hash (`#`).

## stdemo.HTML()

Transform `tagname()` forms to `<tagname>` and `</tagname>` tags, named values to tag attributes, and unnamed values to child nodes.
Quote attribute values with `"`.

Example input:

```
stdemo.HTML(
  p('Structured ' span(style:'font-weight:bold' 'Data Input') ' Notation')
)
```

Example output:

```
<p>Structured <span style="font-weight:bold">Data Input</span> Notation</p>
```

## stdemo.GoImport()

The input is a list of packages, structure it into a sorted list of 3 sections:

- unnamed imports
- named imports (sorted by the name)
- blank imports

Example input:

```
stdemo.GoImport(
  _:            'example.com/groupserver/initserver'
  userpb:       'example.com/groupmanager/user_generated_proto'
  membershippb: 'example.com/groupmanager/membership_generated_proto'
  lookuppb:     'example.com/groupserver/lookup_generated_proto'
  grouppb:      'example.com/groupmanager/group_generated_proto'
  'os'
  'strings'
  'bytes'
  _:            'embed'
)
```

Example output:

```
import(
	"bytes"
	"os"
	"strings"

	grouppb "example.com/groupmanager/group_generated_proto"
	lookuppb "example.com/groupserver/lookup_generated_proto"
	membershippb "example.com/groupmanager/membership_generated_proto"
	userpb "example.com/groupmanager/user_generated_proto"

	_ "embed"
	_ "example.com/groupserver/initserver"
)
```

## stdemo.Groups()

The input is a mapping of groups to member lists.
Validate the input and emit the expanded descriptions as JSON, sorted by the group name.
Emit raw user and group IDs in the JSON.
Map names to IDs as follows:

- Each letter maps to a 5-bit number.
  `a` is 1, `z` is 26.
- Each name is at most 10 characters, so it needs at most 50 bits.
- The bottom 5 bits are the first character, the second 5 bits are the second character, etc., and the rest are zeroes.
- Examples: `u/a` maps to `0b_00001 = 1`, `u/ab` maps to `0b_00010_00001 = 65`.
- Group names map to negative numbers. Example: `g/ab` maps to `-65`.

Furthermore, expand group references recursively.
Error out if the input refers to a nonexistent group.
Also map the group's comment to a description field.

Example input:

```
stdemo.Groups(
  # The fully trained employees.
  g/developers: [u/alice]
  # The people with write access to the source repository.
  g/committers: [g/developers]
)
```

Example output:

```
{
	"-688488233481699": {
		"description": "The people with write access to the source repository.",
		"members": [
			5350785
		]
	},
	"-688483773634724": {
		"description": "The fully trained employees.",
		"members": [
			5350785
		]
	}
}
```
