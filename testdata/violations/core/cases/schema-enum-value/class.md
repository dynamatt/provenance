---
id: ERR-0014
type: ErrorClass
title: A value not in its enum
rule: schema/enum-value
severity: error
examples:
  - file: core/cases/schema-enum-value/violation.md
    expect: violation
  - file: core/cases/schema-enum-value/clean.md
    expect: clean
---
In a field and in a list of the enum.
