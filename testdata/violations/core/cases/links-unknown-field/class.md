---
id: ERR-0018
type: ErrorClass
title: A field reference to a field the target does not have
rule: links/unknown-field
severity: error
examples:
  - file: core/cases/links-unknown-field/violation.md
    expect: violation
  - file: core/cases/links-unknown-field/clean.md
    expect: clean
---
`[[ID#field]]` naming a field the target's type does not have.
