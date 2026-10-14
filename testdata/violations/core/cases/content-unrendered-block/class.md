---
id: ERR-0025
type: ErrorClass
title: A block this version publishes only as source
rule: content/unrendered-block
severity: error
examples:
  - file: core/cases/content-unrendered-block/violation.md
    expect: violation
  - file: core/cases/content-unrendered-block/clean.md
    expect: clean
---
`mermaid` and `drawio` (DES-0036).
