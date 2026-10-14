---
id: ERR-0026
type: ErrorClass
title: A template that cannot render
rule: content/template
severity: error
examples:
  - file: core/templates/BrokenTemplate.tmpl
    expect: violation
  - file: core/schema/TemplateNameClash.yaml
    expect: violation
  - file: core/cases/content-template/violation.md
    expect: violation
  - file: core/templates/ErrorClass.tmpl
    expect: clean
  - file: core/schema/TemplateNameClean.yaml
    expect: clean
  - file: core/cases/content-template/clean.md
    expect: clean
---
One that does not parse, a field whose template name clashes with the engine's, a query naming a template that does not exist.
