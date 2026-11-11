---
id: ERR-0038
type: ErrorClass
title: A CSV file or row that cannot be read
rule: content/csv-row
severity: error
examples:
  - file: core/cases/content-csv-row/violation.csv
    expect: violation
  - file: core/cases/content-csv-row/clean.csv
    expect: clean
---
A row with more or fewer cells than the header, and a row with no ID. The file is read strictly (RFC 4180): a cell holding the delimiter or a quote is quoted, with the quote doubled.
