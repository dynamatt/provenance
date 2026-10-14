---
id: ERR-0023
type: ErrorClass
title: An embed inside running text
rule: content/embed
severity: error
examples:
  - file: core/cases/content-embed/violation.md
    expect: violation
  - file: core/cases/content-embed/clean.md
    expect: clean
---
`![[ID]]` mid-sentence is shown as a reference.
