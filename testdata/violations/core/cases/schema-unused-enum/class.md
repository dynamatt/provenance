---
id: ERR-0008
type: ErrorClass
title: An enum no field uses
rule: schema/unused-enum
severity: warning
examples:
  - file: core/schema/enums/UnusedEnum.yaml
    expect: violation
  - file: core/schema/enums/Expectation.yaml
    expect: clean
---

