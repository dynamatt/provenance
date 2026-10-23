---
id: ERR-0035
type: ErrorClass
title: A rule without a message
rule: rules/message
severity: error
examples:
  - file: core/rules/rules-message-violation.yaml
    expect: violation
  - file: core/rules/rules-clean.yaml
    expect: clean
---
No `message:`, the text each violation is reported with.
