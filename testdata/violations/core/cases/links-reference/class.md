---
id: ERR-0018
type: ErrorClass
title: A reference to a field, tag or caption the target does not have
rule: links/reference
severity: error
examples:
  - file: core/cases/links-reference/violation.md
    expect: violation
  - file: core/cases/links-reference/clean.md
    expect: clean
---
`[[ID#name]]` naming no field, facet, tag or caption of the target, `[[#tag]]`
naming none of the writing entity's own, a mode other than `full`, `number`,
`title` or `long`, and a mode on a field reference.
