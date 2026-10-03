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
			`schema/R.yaml:3: field "rows": a list needs fields: for its rows, or of: naming its item type`},
		{"list with fields and of", "schema/R.yaml", "type: R\nfields:\n  - name: rows\n    type: list\n    of: string\n    fields:\n      - {name: x, type: string}\n",
			`schema/R.yaml:5: field "rows": a list takes fields: or of:, not both`},
		{"of on a non-list", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: string, of: number}\n",
			`schema/R.yaml:3: field "a": of: applies only to type: list`},
		{"unknown item type", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: list, of: strnig}\n",
			`schema/R.yaml:3: field "a": unknown list item type "strnig" (expected string, text, number, date, boolean, or an enum: ApprovalStatus, or a record: Equipment)`},
		{"list of links", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: list, of: link}\n",
			`schema/R.yaml:3: field "a": a list of links is declared as type: link with cardinality: many`},
		{"list of entities", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: list, of: R}\n",
			`schema/R.yaml:3: field "a": a list cannot hold R entities; link to them with type: link, target: [R], cardinality: many`},
		{"record as a field type", "schema/R.yaml", "type: R\nfields:\n  - {name: a, type: Equipment}\n",
			`schema/R.yaml:3: field "a": record Equipment holds a list's rows; declare type: list with of: Equipment`},
		{"bad record field", "schema/records/Part.yaml", "record: Part\nfields:\n  - {name: x, type: nope}\n",
			`schema/records/Part.yaml:3: field "x" in record "Part": unknown type "nope"`},
		{"body in a record", "schema/records/Part.yaml", "record: Part\nfields:\n  - {name: x, type: text, body: true}\n",
			`schema/records/Part.yaml:3: field "x" in record "Part": body: true is only allowed on a top-level text field`},
		{"missing record name", "schema/records/Part.yaml", "fields: []\n", "schema/records/Part.yaml:1: missing record name (record: <Name>)"},
		{"record named like builtin", "schema/records/Part.yaml", "record: date\n", "schema/records/Part.yaml:1: record date has the same name as a built-in field type"},
		{"record named like enum", "schema/records/Part.yaml", "record: ApprovalStatus\n", "schema/records/Part.yaml:1: record ApprovalStatus has the same name as the enum declared in schema/enums/ApprovalStatus.yaml"},
		{"duplicate record", "schema/records/Part.yaml", "record: Equipment\n", "schema/records/Part.yaml:1: record Equipment is already declared in schema/records/Equipment.yaml"},
		{"type named like record", "schema/S.yaml", "type: Equipment\n", "schema/S.yaml:1: type Equipment has the same name as the record declared in schema/records/Equipment.yaml"},
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
			write(t, root, "schema/records/Equipment.yaml", "record: Equipment\nfields:\n  - {name: name, type: string}\n")
			write(t, root, tc.file, tc.content)
			_, err := Load(root)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("err = %v\nwant prefix %s", err, tc.want)
			}
		})
	}
}

func TestListOf(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/enums/ApprovalStatus.yaml", statusEnum)
	write(t, root, "schema/records/Equipment.yaml", `record: Equipment
fields:
  - {name: name, type: string}
  - {name: parts, type: list, of: Part}
`)
	write(t, root, "schema/records/Part.yaml", "record: Part\nfields:\n  - {name: serial, type: string}\n")
	write(t, root, "schema/Evidence.yaml", `type: Evidence
fields:
  - {name: equipment_used, type: list, of: Equipment}
  - {name: standards, type: list, of: string}
  - {name: readings, type: list, of: number}
  - {name: reviews, type: list, of: ApprovalStatus}
`)
	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ev := s.Types["Evidence"]
	eq := ev.Field("equipment_used")
	if eq.Kind != List || eq.Record != "Equipment" || eq.Elem != nil || len(eq.Fields) != 2 || eq.Fields[0] != s.Records["Equipment"].Fields[0] {
		t.Errorf("equipment_used = %+v, want the Equipment record's rows", eq)
	}
	if parts := eq.Fields[1]; parts.Record != "Part" || len(parts.Fields) != 1 {
		t.Errorf("record field parts = %+v, want the Part record's rows", parts)
	}
	for name, want := range map[string]string{"standards": "string:", "readings": "number:", "reviews": "enum:ApprovalStatus"} {
		f := ev.Field(name)
		if f.Kind != List || f.Elem == nil || f.Fields != nil {
			t.Errorf("%s = %+v, want an item list", name, f)
			continue
		}
		if got := string(f.Elem.Kind) + ":" + f.Elem.EnumName; got != want || f.Elem.Name != name {
			t.Errorf("%s items = %s %q, want %s named %q", name, got, f.Elem.Name, want, name)
		}
	}
}

func TestRecordContainingItselfFails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/records/A.yaml", "record: A\nfields:\n  - {name: bs, type: list, of: B}\n")
	write(t, root, "schema/records/B.yaml", "record: B\nfields:\n  - {name: as, type: list, of: A}\n")
	_, err := Load(root)
	want := `schema/records/B.yaml:3: field "as" in record "B": record A contains itself (A → B → A)`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v\nwant %s", err, want)
	}
}

func TestFacetsFromRecordRows(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/records/Mode.yaml", `record: Mode
fields:
  - {name: control, type: link, target: [Protocol], cardinality: one, reverse_name: verifies}
`)
	write(t, root, "schema/Risk.yaml", "type: Risk\nfields:\n  - {name: modes, type: list, of: Mode}\n")
	write(t, root, "schema/Protocol.yaml", "type: Protocol\n")
	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	f := s.Types["Protocol"].Facets
	if len(f) != 1 || f[0].Name != "verifies" || f[0].Sources[0].Type.Name != "Risk" || f[0].Sources[0].List.Name != "modes" {
		t.Fatalf("Protocol facets = %+v", f)
	}

	// A clash is reported where the link is declared: in the record.
	write(t, root, "schema/Protocol.yaml", "type: Protocol\nfields:\n  - {name: verifies, type: string}\n")
	_, err = Load(root)
	if err == nil || !strings.HasPrefix(err.Error(), `schema/records/Mode.yaml:3: field "control": reverse_name "verifies" repeats`) {
		t.Errorf("err = %v", err)
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

func TestFacetsFromReverseNames(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/Requirement.yaml", `type: Requirement
fields:
  - {name: implements, type: link, target: [UserNeed], cardinality: many, reverse_name: implemented_by}
  - {name: verified_by, type: link, target: [Protocol, Nowhere], cardinality: many, reverse_name: verifies}
  - {name: parent, type: link, target: [Requirement], cardinality: one}
`)
	write(t, root, "schema/Design.yaml", `type: Design
fields:
  - {name: implements, type: link, target: [UserNeed], cardinality: many, reverse_name: implemented_by}
`)
	write(t, root, "schema/Risk.yaml", `type: Risk
fields:
  - name: modes
    type: list
    fields:
      - {name: control, type: link, target: [Protocol], cardinality: one, reverse_name: controls}
`)
	write(t, root, "schema/UserNeed.yaml", "type: UserNeed\n")
	write(t, root, "schema/Protocol.yaml", "type: Protocol\n")

	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	facets := func(typ string) string {
		var out []string
		for _, f := range s.Types[typ].Facets {
			var srcs []string
			for _, src := range f.Sources {
				name := src.Type.Name + "." + src.Field.Name
				if src.List != nil {
					name = src.Type.Name + "." + src.List.Name + "[]." + src.Field.Name
				}
				srcs = append(srcs, name)
			}
			out = append(out, f.Name+"<-"+strings.Join(srcs, "+"))
		}
		return strings.Join(out, " ")
	}
	if got := facets("UserNeed"); got != "implemented_by<-Design.implements+Requirement.implements" {
		t.Errorf("UserNeed facets = %s", got)
	}
	if got := facets("Protocol"); got != "controls<-Risk.modes[].control verifies<-Requirement.verified_by" {
		t.Errorf("Protocol facets = %s", got)
	}
	if got := facets("Requirement"); got != "" {
		t.Errorf("a link without reverse_name gave facets: %s", got)
	}
}

func TestReverseNameRepeatingATargetFieldFails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/Requirement.yaml", `type: Requirement
fields:
  - {name: verified_by, type: link, target: [Protocol], cardinality: many, reverse_name: verifies}
`)
	write(t, root, "schema/Protocol.yaml", `type: Protocol
fields:
  - {name: verifies, type: link, target: [Requirement], cardinality: many}
`)
	_, err := Load(root)
	want := `schema/Requirement.yaml:3: field "verified_by": reverse_name "verifies" repeats the field verifies declared on Protocol (schema/Protocol.yaml:3); declare a link once, on its source side`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v\nwant %s", err, want)
	}
}
