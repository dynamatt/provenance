---
id: ERR-0009
type: ErrorClass
title: A record no list uses
rule: schema/unused-record
severity: warning
examples:
  - file: core/schema/records/UnusedRecord.yaml
    expect: violation
  - file: core/schema/records/Example.yaml
    expect: clean
---
