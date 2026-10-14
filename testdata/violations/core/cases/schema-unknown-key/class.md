---
id: ERR-0005
type: ErrorClass
title: A key the schema language does not have
rule: schema/unknown-key
severity: error
examples:
  - file: core/schema/UnknownKeyViolation.yaml
    expect: violation
  - file: core/schema/enums/Colour.yaml
    expect: violation
  - file: core/schema/UnknownKeyClean.yaml
    expect: clean
  - file: core/schema/enums/Status.yaml
    expect: clean
---
A field with `default:` (fields have no defaults), and an enum with a key other than `enum` and `values`.
