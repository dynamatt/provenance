package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const statusEnum = "enum: ApprovalStatus\nvalues: [draft, approved]\n"

const requirement = `type: Requirement
id_prefix: REQ
fields:
  - name: title
    type: string
    required: true
  - name: statement
    type: text
    body: true
  - name: status
    type: ApprovalStatus
    default: draft
  - name: implements
    type: link
    target: [UserNeed]
    cardinality: many
    reverse_name: implemented_by
  - name: order
    type: number
  - name: failure_modes
    type: list
    fields:
      - name: severity
        type: link
        target: [SeverityLevel]
        cardinality: one
      - name: row_rating
        type: calculated
        formula: "severity.score * 2"
`

func TestLoad(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/enums/ApprovalStatus.yaml", statusEnum)
	write(t, root, "schema/Requirement.yaml", requirement)
	write(t, root, "schema/notes.txt", "ignored")

	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if e := s.Enums["ApprovalStatus"]; e == nil || strings.Join(e.Values, ",") != "draft,approved" {
		t.Errorf("enum = %+v", e)
	}
	req := s.Types["Requirement"]
	if req == nil || req.IDPrefix != "REQ" || req.File != "schema/Requirement.yaml" {
		t.Fatalf("type = %+v", req)
	}
	var names []string
	for _, f := range req.Fields {
		names = append(names, f.Name+":"+string(f.Kind))
	}
	if got := strings.Join(names, " "); got != "title:string statement:text status:enum implements:link order:number failure_modes:list" {
		t.Errorf("fields in order = %s", got)
	}
	if req.BodyField == nil || req.BodyField.Name != "statement" {
		t.Errorf("BodyField = %+v", req.BodyField)
	}
	status := req.Fields[2]
	if status.EnumName != "ApprovalStatus" || status.Default == nil || status.Default.Value != "draft" {
		t.Errorf("status = %+v", status)
	}
	impl := req.Fields[3]
	if impl.Cardinality != "many" || impl.ReverseName != "implemented_by" || strings.Join(impl.Target, ",") != "UserNeed" {
		t.Errorf("implements = %+v", impl)
	}
	fm := req.Fields[5]
	if len(fm.Fields) != 2 || fm.Fields[1].Kind != Calculated || fm.Fields[1].Formula != "severity.score * 2" {
		t.Errorf("failure_modes sub-fields = %+v", fm.Fields)
	}
	if req.Fields[0].Line != 4 {
		t.Errorf("title line = %d, want 4", req.Fields[0].Line)
	}
}

func TestLoadWithoutSchemaFolder(t *testing.T) {
	s, err := Load(t.TempDir())
	if err != nil || len(s.Types) != 0 {
		t.Errorf("Load = %+v, %v", s, err)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name, file, content, want string
	}{
		{"unknown type", "schema/R.yaml", "type: R\nfields:\n  - name: title\n    type: strnig\n",
			`schema/R.yaml:4: field "title": unknown type "strnig" (expected string, text, number, date, boolean, link, list, calculated, or an enum: ApprovalStatus)`},
		{"missing type name", "schema/R.yaml", "id_prefix: R\n", "schema/R.yaml:1: missing type name"},
		{"missing field type", "schema/R.yaml", "type: R\nfields:\n  - name: title\n", `schema/R.yaml:3: field "title": missing type`},
		{"unnamed field", "schema/R.yaml", "type: R\nfields:\n  - type: string\n", "schema/R.yaml:3: field without a name"},
		{"duplicate field", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: string}\n  - {name: a, type: number}\n",
			`schema/R.yaml:4: field "a" is declared twice (first at line 3)`},
		{"two body fields", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: text, body: true}\n  - {name: b, type: text, body: true}\n",
			`schema/R.yaml:4: field "b": only one field may set body: true ("a" already does)`},
		{"body not text", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: string, body: true}\n",
			`schema/R.yaml:3: field "a": body: true is only allowed on a top-level text field`},
		{"link without cardinality", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: link, target: [R]}\n",
			`schema/R.yaml:3: field "a": link cardinality must be one or many, got ""`},
		{"list without fields", "schema/R.yaml", "type: R\nfields:\n  - {name: rows, type: list}\n",
			`schema/R.yaml:3: field "rows": a list needs fields for its rows`},
		{"bad sub-field", "schema/R.yaml", "type: R\nfields:\n  - name: rows\n    type: list\n    fields:\n      - {name: x, type: nope}\n",
			`schema/R.yaml:6: field "x" in list "rows": unknown type "nope"`},
		{"type is not a mapping", "schema/R.yaml", "- a\n", "schema/R.yaml: expected a mapping"},
		{"syntax error", "schema/R.yaml", "type: R\nfields: [\n", "schema/R.yaml:"},
		{"wrong value type", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: string, required: maybe}\n", "schema/R.yaml:3: cannot unmarshal"},
		{"enum named like builtin", "schema/enums/E.yaml", "enum: string\nvalues: [a]\n", "schema/enums/E.yaml:1: enum string has the same name as a built-in field type"},
		{"type named like enum", "schema/S.yaml", "type: ApprovalStatus\n", "schema/S.yaml:1: type ApprovalStatus has the same name as the enum declared in schema/enums/ApprovalStatus.yaml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "schema/enums/ApprovalStatus.yaml", statusEnum)
			write(t, root, tc.file, tc.content)
			_, err := Load(root)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("err = %v\nwant prefix %s", err, tc.want)
			}
		})
	}
}

func TestDuplicateTypeAcrossFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/A.yaml", "type: Requirement\n")
	write(t, root, "schema/B.yaml", "type: Requirement\n")
	_, err := Load(root)
	if err == nil || err.Error() != "schema/B.yaml:1: type Requirement is already declared in schema/A.yaml" {
		t.Errorf("err = %v", err)
	}
}
