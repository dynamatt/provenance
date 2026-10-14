---
id: ERR-0004
type: ErrorClass
title: An ID that is not the type's form
rule: schema/id-format
severity: error
examples:
  - file: core/cases/schema-id-format/violation.md
    expect: violation
  - file: core/cases/schema-id-format/violation-page-name.md
    expect: violation
  - file: core/schema/NoPrefix.yaml
    expect: violation
  - file: core/schema/DuplicatePrefixA.yaml
    expect: violation
  - file: core/schema/DuplicatePrefixB.yaml
    expect: violation
  - file: core/cases/schema-id-format/clean.md
    expect: clean
  - file: core/schema/IdFormatClean.yaml
    expect: clean
---
An entity ID that is not `PREFIX-NNNN` or cannot be a page name; a type with no `id_prefix`; two types with one `id_prefix`.
