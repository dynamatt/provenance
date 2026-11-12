---
id: ERR-0040
type: ErrorClass
title: A tag that cannot be referred to
rule: content/tag
severity: error
examples:
  - file: core/cases/content-tag/violation.md
    expect: violation
  - file: core/cases/content-tag/clean.md
    expect: clean
---
A tag (`{#tag}`) used twice in one entity, also as a caption id; not
written in lower-case letters, digits and `-`; on something other than a
heading or an ordered list item's first paragraph; or named like a field
or facet of its entity's type, which `[[ID#name]]` would show instead.
