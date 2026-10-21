---
id: ERR-0027
type: ErrorClass
title: A template named for nothing
rule: content/template-name
severity: warning
examples:
  - file: core/templates/NotAType.tmpl
    expect: violation
  - file: core/templates/ErrorClass.tmpl
    expect: clean
---
Neither a declared type nor a kebab-case named template.
