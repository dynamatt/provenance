---
id: ERR-0032
type: ErrorClass
title: A rule file not named for a kebab-case ID
rule: rules/name
severity: error
examples:
  - file: core/rules/Rules_Name_Violation.yaml
    expect: violation
  - file: core/rules/rules-clean.yaml
    expect: clean
---
The file name is the rule's ID, in lower-case kebab-case, with `.yaml` (DES-0016).
