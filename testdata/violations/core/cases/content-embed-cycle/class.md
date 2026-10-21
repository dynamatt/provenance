---
id: ERR-0024
type: ErrorClass
title: Entities that embed each other
rule: content/embed-cycle
severity: error
examples:
  - file: core/cases/content-embed-cycle/violation-a.md
    expect: violation
  - file: core/cases/content-embed-cycle/violation-b.md
    expect: violation
  - file: core/cases/content-embed-cycle/clean.md
    expect: clean
---
Reported at each embed in the cycle.
