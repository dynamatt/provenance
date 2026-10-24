---
id: ERR-0036
type: ErrorClass
title: A rule whose query does not fit the schema
rule: rules/query
severity: error
examples:
  - file: core/rules/rules-query-violation.yaml
    expect: violation
  - file: core/rules/rules-clean.yaml
    expect: clean
---
A `where` naming a field `Widget` does not have, reported as a query block's would be.
