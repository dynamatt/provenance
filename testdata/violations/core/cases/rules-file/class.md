---
id: ERR-0031
type: ErrorClass
title: A rule file that is not a YAML mapping
rule: rules/file
severity: error
examples:
  - file: core/rules/rules-file-violation.yaml
    expect: violation
  - file: core/rules/rules-clean.yaml
    expect: clean
---
Unparsable YAML. A broken rule file skips only itself; the other rules run.
