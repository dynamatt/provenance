---
id: ERR-0028
type: ErrorClass
title: A named template no query chooses
rule: content/unused-template
severity: warning
examples:
  - file: core/templates/spare.tmpl
    expect: violation
  - file: core/templates/widget-row.tmpl
    expect: clean
---

