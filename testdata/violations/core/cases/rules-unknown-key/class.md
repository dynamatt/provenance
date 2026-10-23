---
id: ERR-0033
type: ErrorClass
title: A key a rule file does not have
rule: rules/unknown-key
severity: error
examples:
  - file: core/rules/rules-unknown-key-violation.yaml
    expect: violation
  - file: core/rules/rules-clean.yaml
    expect: clean
---
An `id:` key: a rule's ID is its file name. The keys are `severity`, `message`, `from` and `where`.
