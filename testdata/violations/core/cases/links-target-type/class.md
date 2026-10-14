---
id: ERR-0019
type: ErrorClass
title: A link to an entity of the wrong type
rule: links/target-type
severity: error
examples:
  - file: core/cases/links-target-type/violation.md
    expect: violation
  - file: core/cases/links-target-type/clean.md
    expect: clean
---
A link field pointing at a type outside its `target:`.
