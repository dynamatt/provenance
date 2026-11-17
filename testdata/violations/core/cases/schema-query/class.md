---
id: ERR-0042
type: ErrorClass
title: A query field whose query cannot run
rule: schema/query
severity: error
examples:
  - file: core/cases/schema-query/violation.md
    expect: violation
  - file: core/cases/schema-query/violation-field.md
    expect: violation
  - file: core/cases/schema-query/violation-render.md
    expect: violation
  - file: core/cases/schema-query/violation-loop.md
    expect: violation
  - file: core/cases/schema-query/clean.md
    expect: clean
---
A query field's query that is not a mapping, has a key a query field doesn't have (`render` and `template` included: its entity's template shows the results), selects from a type outside the field's `of:`, names a field the types don't have, or depends on its own results through `missing`, `not_in`, `not_equals` or an aggregation, so it has no answer.
