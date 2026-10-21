---
id: ERR-0002
type: ErrorClass
title: Two files with one ID
rule: content/duplicate-id
severity: error
examples:
  - file: core/cases/content-duplicate-id/violation-a.md
    expect: violation
  - file: core/cases/content-duplicate-id/violation-b.md
    expect: violation
  - file: core/cases/content-duplicate-id/clean.md
    expect: clean
---
Every file declaring the ID is reported, naming the others.
