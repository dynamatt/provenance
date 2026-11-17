---
id: ERR-0030
type: ErrorClass
title: A schema file that cannot be loaded
rule: schema/file
severity: error
examples:
  - file: broken-schema/schema/Broken.yaml
    expect: violation
  - file: broken-schema/schema/UnknownType.yaml
    expect: violation
  - file: broken-schema/schema/CsvListOfRows.yaml
    expect: violation
  - file: broken-schema/schema/FieldName.yaml
    expect: violation
  - file: broken-schema/schema/ExtendsUnknown.yaml
    expect: violation
  - file: broken-schema/schema/CycleA.yaml
    expect: violation
  - file: broken-schema/schema/CycleB.yaml
    expect: violation
  - file: broken-schema/schema/Redeclared.yaml
    expect: violation
  - file: broken-schema/schema/AbstractPrefix.yaml
    expect: violation
  - file: broken-schema/schema/QueryInCsv.yaml
    expect: violation
  - file: broken-schema/schema/QueryWithoutOf.yaml
    expect: violation
  - file: broken-schema/schema/Fine.yaml
    expect: clean
  - file: broken-schema/schema/FineChild.yaml
    expect: clean
---
Every other check is read against the schema, so this one lives in its own repository, broken-schema: with it, the rest are not run.
