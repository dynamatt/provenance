---
id: ERR-0034
type: ErrorClass
title: A rule severity other than error or warning
rule: rules/severity
severity: error
examples:
  - file: core/rules/rules-severity-violation.yaml
    expect: violation
  - file: core/rules/rules-clean.yaml
    expect: clean
---
`severity: fatal`.
