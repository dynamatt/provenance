---
id: ERR-0039
type: ErrorClass
title: A CSV column no field reads, or a field with no column
rule: content/csv-column
severity: warning
examples:
  - file: core/cases/content-csv-column/violation.csv
    expect: violation
  - file: core/cases/content-csv-column/clean.csv
    expect: clean
---
A column the schema doesn't name is ignored, and a field whose column is missing is empty on every row; both are reported so a column renamed in the source system is noticed.
