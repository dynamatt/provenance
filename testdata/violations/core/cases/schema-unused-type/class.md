---
id: ERR-0010
type: ErrorClass
title: A type with no entities and no links
rule: schema/unused-type
severity: warning
examples:
  - file: core/schema/UnusedTypeViolation.yaml
    expect: violation
  - file: core/schema/UnusedTypeClean.yaml
    expect: clean
---
A type in a chain of links is used even before it has entities, so only a type with neither is reported.
