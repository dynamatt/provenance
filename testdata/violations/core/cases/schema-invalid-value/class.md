---
id: ERR-0015
type: ErrorClass
title: A value not of its field's type
rule: schema/invalid-value
severity: error
examples:
  - file: core/cases/schema-invalid-value/violation.md
    expect: violation
  - file: core/cases/schema-invalid-value/clean.md
    expect: clean
---
A date that is not a date, a number that is not a number.
