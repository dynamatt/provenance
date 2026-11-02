---
id: ERR-0040
type: ErrorClass
title: A dump without a sidecar that describes it
rule: content/csv-sidecar
severity: error
examples:
  - file: core/cases/content-csv-sidecar/violation.csv.yaml
    expect: violation
  - file: core/cases/content-csv-sidecar/violation-missing.csv
    expect: violation
  - file: core/cases/content-csv-sidecar/clean.csv
    expect: clean
  - file: core/cases/content-csv-sidecar/clean.csv.yaml
    expect: clean
---
A dump of another system (a source with `system:`) has a sidecar saying when it was exported, with the file's SHA-256, so a new dump can't be committed without saying when it was exported.
