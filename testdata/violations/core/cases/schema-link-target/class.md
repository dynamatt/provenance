---
id: ERR-0006
type: ErrorClass
title: A link field targeting an undeclared type
rule: schema/link-target
severity: error
examples:
  - file: core/schema/LinkTargetViolation.yaml
    expect: violation
  - file: core/schema/LinkTargetClean.yaml
    expect: clean
---
A `target:` names a type `schema/` does not declare.
