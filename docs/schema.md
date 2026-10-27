# schema requirements

This task defines a hypothetical schema syntax for validating sdin configs.
This schema is defined in sdin itself and uses forms for types.
The input contains a set of types and typed constants.
Print OK if the types are correctly defined and the constants correctly represent the type's requirements.

Example input:

```
meta.PropertyPackages: [checks]

# A type definition.
Date: struct({
  Year:  int()
  Month: identifier(checks.MatchToken('[A-Z][a-z]+'))
  Day:   int(checks.IntRange(1 31))
})

# A constant definition.
NewYear:  const(Date {Year:2006 Month:February Day:3})
```

Example output:

```
OK
```

Note that this is a hard and underspecified task.
The rules are uncovered through the testcases, so the challenge here is to reverse engineer them based on the test output.
