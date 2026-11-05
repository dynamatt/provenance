package load

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dynamatt/provenance/internal/entity"
	"github.com/dynamatt/provenance/internal/model"
)

// The CSV source (DES-0053). A row is an entity whose cells are typed by
// their fields exactly as an entity file's values are.

const complaintSchema = `type: Complaint
id_prefix: CPL
source:
  csv: data/complaints.csv
  id: Complaint No
fields:
  - {name: received, type: date, column: Date Received}
  - {name: product, type: string}
  - {name: harm, type: boolean}
  - {name: count, type: number}
  - {name: summary, type: text}
  - {name: parts, type: link, target: [Part], cardinality: many, reverse_name: complaints}
  - {name: tags, type: list, of: string}
  - {name: double, type: calculated, formula: "count * 2"}
`

func complaintRepo(csv string, extra map[string]string) map[string]string {
	files := map[string]string{
		"schema/Part.yaml":      partSchema,
		"schema/Complaint.yaml": complaintSchema,
		"PRT/PRT-0001.md":       part("PRT-0001", ""),
		"PRT/PRT-0002.md":       part("PRT-0002", ""),
		"data/complaints.csv":   csv,
	}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

const complaintsCSV = "\uFEFFComplaint No,Date Received,product,harm,count,summary,parts,tags,unused\r\n" +
	"CPL-2026-0141,2026-08-14,\"Lead kit, long\",false,2,\"Signal lost.\r\nSee [[PRT-0001]].\",PRT-0001; PRT-0002,a;b,x\r\n" +
	"CPL-2026-0142,,Charger,TRUE,,,,,\r\n"

func TestCSVLoad(t *testing.T) {
	res := loadRepo(t, complaintRepo(complaintsCSV, nil))
	if got := problems(res); !slices.Equal(got, []found{
		{"content/csv-column", "data/complaints.csv", 1, false},
	}) {
		t.Fatalf("problems: %v", res.Problems)
	}
	var rows []*model.Entity
	for _, e := range res.Entities {
		if e.Type == "Complaint" {
			rows = append(rows, e)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows: %d", len(rows))
	}
	r := rows[0]
	if r.ID != "CPL-2026-0141" || r.Path != "data/complaints.csv" || r.IDLine() != 2 {
		t.Errorf("row: %s %s:%d", r.ID, r.Path, r.IDLine())
	}
	check := func(name string, want any) {
		t.Helper()
		v := r.Field(name)
		var got any
		switch {
		case v.Invalid:
			got = "invalid: " + v.Problem
		case !v.Present:
			got = nil
		case v.Field.Kind == "number" || v.Field.Kind == "calculated":
			got = v.Num
		case v.Field.Kind == "boolean":
			got = v.Bool
		case v.Field.Kind == "link":
			got = strings.Join(v.IDs, ",")
		case v.Field.Kind == "list":
			var items []string
			for _, it := range v.Items {
				items = append(items, it.Str)
			}
			got = strings.Join(items, ",")
		default:
			got = v.Str
		}
		if got != want {
			t.Errorf("%s: got %#v, want %#v", name, got, want)
		}
	}
	check("received", "2026-08-14")
	check("product", "Lead kit, long")
	check("harm", false)
	check("count", 2.0)
	check("summary", "Signal lost.\nSee [[PRT-0001]].")
	check("parts", "PRT-0001,PRT-0002")
	check("tags", "a,b")
	check("double", 4.0)
	if v := r.Field("summary"); v.Written.TextLine != 2 {
		t.Errorf("summary text line: %d", v.Written.TextLine)
	}
	refs, err := r.Record.Refs(r.Field("summary").Str)
	if err != nil || !slices.Equal(refs.Cited, []string{"PRT-0001"}) {
		t.Errorf("refs: %v %v", refs, err)
	}
	// Empty cells are fields not written.
	if r2 := rows[1]; r2.Field("received").Present || !r2.Field("harm").Bool || r2.Field("parts").Present {
		t.Errorf("empty cells: %+v", r2.Fields)
	}
	// A row links out, and the target's facet lists it.
	var prt *model.Entity
	for _, e := range res.Entities {
		if e.ID == "PRT-0002" {
			prt = e
		}
	}
	if in := prt.Facet("complaints"); in == nil || len(in.From) != 1 || in.From[0].ID != "CPL-2026-0141" {
		t.Errorf("facet: %+v", in)
	}
	// The file.
	if len(res.Tables) != 1 {
		t.Fatalf("tables: %v", res.Tables)
	}
	tb := res.Tables[0]
	if tb.Path != "data/complaints.csv" || tb.Type != "Complaint" {
		t.Errorf("table: %+v", tb)
	}
	if row, ok := r.Record.(*entity.Row); !ok || row.Table() != tb {
		t.Errorf("row table")
	}
	files, _ := res.Content().Files()
	if !slices.Equal(files, []string{"PRT/PRT-0001.md", "PRT/PRT-0002.md", "data/complaints.csv"}) {
		t.Errorf("content files: %v", files)
	}
}

// A value is valid in a cell exactly when it is valid in an entity file,
// and means the same.
func TestCellsTypedAsInAFile(t *testing.T) {
	fields := []struct{ decl, yaml, cell string }{
		{"{name: v, type: string}", "v: plain", "plain"},
		{"{name: v, type: text}", "v: \"*emphasis*\"", "*emphasis*"},
		{"{name: v, type: number}", "v: 2.5", "2.5"},
		{"{name: v, type: number}", "v: -3", "-3"},
		{"{name: v, type: number}", "v: 1e3", "1e3"},
		{"{name: v, type: number}", "v: \"1,000\"", "1,000"},
		{"{name: v, type: number}", "v: 5%", "5%"},
		{"{name: v, type: number}", "v: 12 mm", "12 mm"},
		{"{name: v, type: date}", "v: 2026-08-14", "2026-08-14"},
		{"{name: v, type: date}", "v: 14/08/2026", "14/08/2026"},
		{"{name: v, type: boolean}", "v: true", "true"},
		{"{name: v, type: boolean}", "v: FALSE", "FALSE"},
		{"{name: v, type: boolean}", "v: \"yes\"", "yes"},
		{"{name: v, type: boolean}", "v: \"1\"", "1"},
		{"{name: v, type: Colour}", "v: red", "red"},
		{"{name: v, type: link, target: [Part], cardinality: one}", "v: PRT-0001", "PRT-0001"},
		{"{name: v, type: link, target: [Part], cardinality: many}", "v: [PRT-0001, PRT-0002]", "PRT-0001;PRT-0002"},
		{"{name: v, type: link, target: [Part], cardinality: many}", "v: [PRT-0001, ]", "PRT-0001;"},
		{"{name: v, type: list, of: number}", "v: [1, 2]", "1;2"},
		{"{name: v, type: list, of: number}", "v: [1, two]", "1;two"},
		{"{name: v, type: list, of: Colour}", "v: [red, blue]", "red;blue"},
	}
	for i, c := range fields {
		t.Run(fmt.Sprint(i, " ", c.cell), func(t *testing.T) {
			csv := "id,v\nTAB-0001," + quote(c.cell) + "\n"
			res := loadRepo(t, map[string]string{
				"schema/enums/Colour.yaml": "enum: Colour\nvalues: [red, blue]\n",
				"schema/Part.yaml":         partSchema,
				"schema/Doc.yaml":          "type: Doc\nid_prefix: DOC\nfields:\n  - " + c.decl + "\n",
				"schema/Tab.yaml":          "type: Tab\nid_prefix: TAB\nsource: {csv: t.csv, id: id}\nfields:\n  - " + c.decl + "\n",
				"PRT/PRT-0001.md":          part("PRT-0001", ""),
				"PRT/PRT-0002.md":          part("PRT-0002", ""),
				"DOC/DOC-0001.md":          "---\nid: DOC-0001\ntype: Doc\n" + c.yaml + "\n---\n",
				"t.csv":                    csv,
			})
			if len(res.Problems) > 0 {
				t.Fatalf("problems: %v", res.Problems)
			}
			var doc, tab *model.Value
			for _, e := range res.Entities {
				switch e.ID {
				case "DOC-0001":
					doc = e.Field("v")
				case "TAB-0001":
					tab = e.Field("v")
				}
			}
			if got, want := describe(tab), describe(doc); got != want {
				t.Errorf("cell %q: %s\nentity file %q: %s", c.cell, got, c.yaml, want)
			}
		})
	}
}

func quote(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// describe is what model makes of a value, its position aside.
func describe(v *model.Value) string {
	if v.Invalid {
		return "invalid: " + v.Problem
	}
	items := []string{}
	for _, it := range v.Items {
		items = append(items, describe(it))
	}
	return fmt.Sprintf("present=%v str=%q num=%v bool=%v ids=%v items=%v", v.Present, v.Str, v.Num, v.Bool, v.IDs, items)
}

func TestCSVProblems(t *testing.T) {
	const tab = "type: Tab\nid_prefix: TAB\nsource: {csv: t.csv, id: id}\nfields:\n  - {name: title, type: string}\n"
	cases := []struct {
		name  string
		files map[string]string
		want  []found
		msg   string
	}{
		{"missing file", map[string]string{"schema/Tab.yaml": tab},
			[]found{{"content/csv-row", "schema/Tab.yaml", 3, true}}, "no file t.csv, the source of type Tab"},
		{"empty", map[string]string{"schema/Tab.yaml": tab, "t.csv": ""},
			[]found{{"content/csv-row", "t.csv", 1, true}}, "the file is empty"},
		{"no ID column", map[string]string{"schema/Tab.yaml": tab, "t.csv": "key,title\n"},
			[]found{{"content/csv-row", "t.csv", 1, true}}, `no column "id"`},
		{"header twice", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,title,title\n"},
			[]found{{"content/csv-row", "t.csv", 1, true}}, `column "title" is in the header twice`},
		{"cells", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,title\nTAB-0001,a,b\nTAB-0002\nTAB-0003,c\n"},
			[]found{{"content/csv-row", "t.csv", 2, true}, {"content/csv-row", "t.csv", 3, true}}, "the row has 3 cells, but the header has 2"},
		{"bare quote stops", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,title\nTAB-0001,a \"b\"\nTAB-0002,c\n"},
			[]found{{"content/csv-row", "t.csv", 2, true}}, "a quote inside a cell that is not quoted"},
		{"not UTF-8", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,title\nTAB-0001,\xff\n"},
			[]found{{"content/csv-row", "t.csv", 2, true}}, "not UTF-8"},
		{"no ID", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,title\n ,a\n"},
			[]found{{"content/csv-row", "t.csv", 2, true}}, `the row has no ID in column "id"`},
		{"duplicate ID", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,title\nTAB-0001,a\nTAB-0001,b\n"},
			[]found{{"content/duplicate-id", "t.csv", 2, true}, {"content/duplicate-id", "t.csv", 3, false}}, "duplicate entity ID TAB-0001, also in t.csv:3"},
		{"duplicate across sources", map[string]string{"schema/Tab.yaml": tab, "schema/Note.yaml": "type: Note\nid_prefix: TAB\nfields: []\n",
			"t.csv": "id,title\nTAB-0001,a\n", "n.md": "---\nid: TAB-0001\ntype: Note\n---\n"},
			[]found{{"content/duplicate-id", "n.md", 2, true}, {"content/duplicate-id", "t.csv", 2, false}}, "duplicate entity ID TAB-0001, also in t.csv"},
		{"Markdown file of a CSV type", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,title\n", "x.md": "---\nid: TAB-0009\ntype: Tab\n---\n"},
			[]found{{"content/frontmatter", "x.md", 3, true}}, "type Tab is read from t.csv"},
		{"columns", map[string]string{"schema/Tab.yaml": tab, "t.csv": "id,name\n"},
			[]found{{"content/csv-column", "t.csv", 1, false}, {"content/csv-column", "t.csv", 1, false}}, `no column "title"`},
		{"delimiter", map[string]string{"schema/Tab.yaml": strings.Replace(tab, "id: id}", "id: id, delimiter: \";\"}", 1), "t.csv": "id;title\nTAB-0001;a, b\n"},
			nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := loadRepo(t, c.files)
			if got := problems(res); !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", res.Problems, c.want)
			}
			if c.msg != "" && !strings.Contains(res.Problems[0].Msg, c.msg) {
				t.Errorf("message %q, want %q", res.Problems[0].Msg, c.msg)
			}
		})
	}
}

// History compares a row by the cells its fields read, so a dump that
// reorders or adds columns changes no row (C7).
func TestCSVHistoryEntities(t *testing.T) {
	res := loadRepo(t, complaintRepo(complaintsCSV, nil))
	c := res.Content()
	read := func(s string) func() ([]byte, error) { return func() ([]byte, error) { return []byte(s), nil } }
	before, err := c.Entities("data/complaints.csv", read(complaintsCSV))
	if err != nil || len(before) != 2 {
		t.Fatalf("%v %v", before, err)
	}
	reordered := "unused,new,Complaint No,Date Received,product,harm,count,summary,parts,tags\n" +
		"y,z,CPL-2026-0141,2026-08-14,\"Lead kit, long\",false,2,\"Signal lost.\nSee [[PRT-0001]].\",PRT-0001; PRT-0002,a;b\n" +
		"y,z,CPL-2026-0142,,Charger,TRUE,,,,\n"
	after, _ := c.Entities("data/complaints.csv", read(reordered))
	if !slices.Equal(before, after) {
		t.Errorf("reordered columns changed rows:\n%v\n%v", before, after)
	}
	edited, _ := c.Entities("data/complaints.csv", read(strings.Replace(reordered, "Charger", "Charger cable", 1)))
	if edited[0] != before[0] || edited[1] == before[1] {
		t.Errorf("an edited row: %v", edited)
	}
	if got, err := c.Entities("data/other.csv", read("")); got != nil || err != nil {
		t.Errorf("another file: %v %v", got, err)
	}
}
