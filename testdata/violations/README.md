# Violations

One example of everything `provenance validate` reports, and next to each,
the same thing written correctly, which must not be reported. The example
repository (`provenance-example`) shows a design history file that passes.
This folder shows each way one can fail.

```bash
make build
bin/provenance validate --path testdata/violations/core           # 48 errors, 6 warnings
bin/provenance validate --path testdata/violations/broken-schema  # 2 errors
```

The expected reports are `testdata/validate/violations-core.txt` and
`violations-broken-schema.txt`.

## Layout

- `core/` is a repository holding every check but one.
  - `cases/<rule>/` holds one check. `cases/schema-required/` is
    `schema/required`. In each folder:
    - `class.md` is an `ErrorClass` entity: the rule, its severity and its
      examples, each a file marked `violation` or `clean`.
    - `violation*.md` and `clean.md` are the entity examples.
  - The examples that can't be entities are in `schema/` and `templates/`,
    named for their case (`schema/FormulaViolation.yaml`,
    `schema/FormulaClean.yaml`), and the `class.md` lists them.
  - Most entity examples are `Widget`s (`schema/Widget.yaml`), which has
    a field of each kind.
- `broken-schema/` is a repository for `schema/file`. A schema that can't
  be loaded stops every check made against it, so it can't share a
  repository with them. Its `ErrorClass` is in `core/cases/schema-file/`.

Example paths in `class.md` are relative to this folder (`core/…`,
`broken-schema/…`).

## What the tests hold it to

`internal/validate/violations_test.go`:

- Each `violation` file is reported for its class's rule, at its class's
  severity.
- Each `clean` file is not reported at all.
- Every class has at least one of each.
- Every rule ID the code reports (`"content/…"`, `"links/…"`, `"schema/…"`,
  `"rules/…"` in `internal/`) has a class, and every class's rule is in the
  code.
- The whole report matches `testdata/validate/violations-*.txt`, which
  `scripts/acceptance.sh` also checks against the binary.

## Adding a check

When validate gets a new check:

1. Add `core/cases/<rule>/` with a `class.md` (the next `ERR-NNNN`), a
   `violation.md` and a `clean.md`. Use the next free `WID-NNNN`, or put the
   example in `schema/` or `templates/` if it can't be an entity.
2. A clean example must be clean for every check, not only its own.
3. Run `make golden-update` and look at the diff in
   `testdata/validate/violations-core.txt`.
