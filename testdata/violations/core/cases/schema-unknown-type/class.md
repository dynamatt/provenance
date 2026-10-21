---
id: ERR-0003
type: ErrorClass
title: An entity of an undeclared type
rule: schema/unknown-type
severity: error
examples:
  - file: core/cases/schema-unknown-type/violation.md
    expect: violation
  - file: core/cases/schema-unknown-type/clean.md
    expect: clean
---
The entity's `type` is not declared in `schema/`.
