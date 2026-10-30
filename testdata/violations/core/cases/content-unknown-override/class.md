---
id: ERR-0029
type: ErrorClass
title: An underscore template that overrides nothing
rule: content/unknown-override
severity: warning
examples:
  - file: core/templates/_footer.tmpl
    expect: violation
  - file: core/templates/_cite.tmpl
    expect: clean
---
