---
id: ERR-0037
type: ErrorClass
title: An entity file not in canonical form
rule: content/format
severity: warning
examples:
  - file: core/cases/content-format/violation.md
    expect: violation
  - file: core/cases/content-format/clean.md
    expect: clean
---
An entity file is in canonical form when `provenance fmt` would leave it unchanged (DES-0038).
