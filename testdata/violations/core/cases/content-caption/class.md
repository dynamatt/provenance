---
id: ERR-0022
type: ErrorClass
title: A caption that cannot be placed or referred to
rule: content/caption
severity: error
examples:
  - file: core/cases/content-caption/violation.md
    expect: violation
  - file: core/cases/content-caption/clean.md
    expect: clean
---
Misplaced, malformed, an id used twice on one page, a `[[#id]]` with no caption on its page.
