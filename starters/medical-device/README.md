# @NAME@

The design history file of @NAME@, kept with
[Provenance](https://github.com/dynamatt/provenance). Every record is a
file in this repository, and every change to one is a reviewed commit.

| Folder | Holds |
| --- | --- |
| `USR/` | User needs |
| `REQ/` | Requirements: design inputs |
| `DES/` | Design outputs |
| `RSK/` | Risks |
| `VER/` | Verification protocols and their evidence |
| `VAL/` | Validation evidence |
| `ECO/` | Change requests |
| `REF/` | Standards and other external documents |
| `DOC/` | Documents assembled from the records |
| `schema/` | The record types and their fields |
| `rules/` | The checks a design review makes |

Every entity marked "Example" is a worked example of how records link:
replace each with your own, or delete it.

```bash
provenance validate              # every check, at its file and line
provenance fmt                   # canonical form for the files you changed
provenance export website        # the site, in _site/
```
