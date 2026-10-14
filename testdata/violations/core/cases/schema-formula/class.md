---
id: ERR-0007
type: ErrorClass
title: A calculated field whose formula cannot run
rule: schema/formula
severity: error
examples:
  - file: core/schema/FormulaViolation.yaml
    expect: violation
  - file: core/schema/FormulaClean.yaml
    expect: clean
---
The formula names a field the type does not have.
