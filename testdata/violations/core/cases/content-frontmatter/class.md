---
id: ERR-0001
type: ErrorClass
title: Frontmatter that cannot be read
rule: content/frontmatter
severity: error
examples:
  - file: core/cases/content-frontmatter/violation-yaml.md
    expect: violation
  - file: core/cases/content-frontmatter/violation-no-type.md
    expect: violation
  - file: core/cases/content-frontmatter/clean.md
    expect: clean
---
A file whose frontmatter is not YAML, or has an `id` but no `type`, is not read as an entity.
