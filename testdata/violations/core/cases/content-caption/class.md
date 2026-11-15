---
id: ERR-0022
type: ErrorClass
title: A caption that cannot be placed
rule: content/caption
severity: error
examples:
  - file: core/cases/content-caption/violation.md
    expect: violation
  - file: core/cases/content-caption/clean.md
    expect: clean
---
Misplaced, malformed, or an id used twice on one page. A `[[#id]]` to no
caption is `links/reference`.
