---
id: ERR-0041
type: ErrorClass
title: An entity of an abstract type
rule: schema/abstract-type
severity: error
examples:
  - file: core/cases/schema-abstract-type/violation.md
    expect: violation
  - file: core/cases/schema-abstract-type/clean.md
    expect: clean
---
The entity's `type` is declared `abstract: true`: it declares fields for the types extending it, and has no entities of its own.
