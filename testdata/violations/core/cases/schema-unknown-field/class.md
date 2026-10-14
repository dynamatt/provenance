---
id: ERR-0011
type: ErrorClass
title: A frontmatter key the type does not have
rule: schema/unknown-field
severity: error
examples:
  - file: core/cases/schema-unknown-field/violation.md
    expect: violation
  - file: core/cases/schema-unknown-field/clean.md
    expect: clean
---
A misspelt field, the body field written in the frontmatter, or an incoming link written on its target.
