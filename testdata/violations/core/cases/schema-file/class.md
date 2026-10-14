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
  - file: broken-schema/schema/Fine.yaml
    expect: clean
---
Every other check is read against the schema, so this one lives in its own repository, broken-schema: with it, the rest are not run.
