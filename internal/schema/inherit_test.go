package schema

import (
	"slices"
	"strings"
	"testing"
)

func TestInheritance(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/Evidence.yaml", `type: Evidence
abstract: true
fields:
  - {name: title, type: string, title: true}
  - {name: notes, type: text, body: true}
  - {name: tool, type: link, target: [Tool], cardinality: one, reverse_name: used_in}
`)
	write(t, root, "schema/Test.yaml", "type: Test\nextends: Evidence\nid_prefix: TST\nfields:\n  - {name: result, type: string}\n")
	write(t, root, "schema/Bench.yaml", "type: Bench\nextends: Test\nfields:\n  - {name: rig, type: string}\n")
	write(t, root, "schema/Field.yaml", "type: Field\nextends: Evidence\nid_prefix: FLD\n")
	write(t, root, "schema/Tool.yaml", "type: Tool\nid_prefix: TL\nfields:\n  - {name: checks, type: link, target: [Evidence], cardinality: many}\n")
	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	names := func(fs []*Field) string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Name)
		}
		return strings.Join(out, " ")
	}
	bench := s.Types["Bench"]
	if got := names(bench.Fields); got != "title notes tool result rig" {
		t.Errorf("Bench fields = %s", got)
	}
	if got := names(bench.Declared); got != "rig" {
		t.Errorf("Bench declared = %s", got)
	}
	if bench.TitleField == nil || bench.TitleField.Name != "title" || bench.BodyField == nil || bench.BodyField.Name != "notes" {
		t.Error("Bench should inherit its title and body fields")
	}
	// A subtype numbers in its parent's series unless it declares its own.
	if bench.IDPrefix != "TST" || bench.PrefixType != s.Types["Test"] {
		t.Errorf("Bench prefix = %s from %v", bench.IDPrefix, bench.PrefixType)
	}
	if s.Types["Evidence"].IDPrefix != "" {
		t.Error("an abstract type has no prefix")
	}
	if !bench.IsA("Evidence") || !bench.IsA("Bench") || bench.IsA("Field") {
		t.Error("IsA")
	}
	if got := bench.Owner(bench.Field("tool")).Name; got != "Evidence" {
		t.Errorf("tool declared on %s", got)
	}
	// A type stands for its subtypes; abstract types have no entities.
	if got := s.Expand([]string{"Evidence"}); !slices.Equal(got, []string{"Bench", "Field", "Test"}) {
		t.Errorf("Expand(Evidence) = %v", got)
	}
	if got := s.Types["Tool"].Field("checks").Targets; !slices.Equal(got, []string{"Bench", "Field", "Test"}) {
		t.Errorf("Targets = %v", got)
	}
	// An inherited link feeds its facet from every concrete type having it.
	used := s.Types["Tool"].Facets
	if len(used) != 1 || used[0].Name != "used_in" || len(used[0].Sources) != 3 {
		t.Fatalf("Tool facets = %+v", used)
	}
	for _, src := range used[0].Sources {
		if src.Type.Abstract {
			t.Errorf("facet source from abstract %s", src.Type.Name)
		}
	}
}

func TestInheritanceProblems(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schema/A.yaml", "type: A\nextends: B\nid_prefix: A\n")
	write(t, root, "schema/B.yaml", "type: B\nextends: A\nid_prefix: B\n")
	write(t, root, "schema/C.yaml", "type: C\nextends: Nope\nid_prefix: C\n")
	write(t, root, "schema/D.yaml", "type: D\nabstract: true\nid_prefix: D\nfields:\n  - {name: x, type: string}\n")
	write(t, root, "schema/E.yaml", "type: E\nextends: D\nid_prefix: E\nfields:\n  - {name: x, type: number}\n")
	write(t, root, "schema/F.yaml", "type: F\nabstract: maybe\nid_prefix: F\n")
	_, problems, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range problems {
		got = append(got, p.Error())
	}
	want := []string{
		`schema/F.yaml:2: type F: abstract: must be true or false`,
		`schema/C.yaml:2: type C: extends: unknown type "Nope" (types: A, B, C, D, E, F)`,
		`schema/A.yaml:2: type A extends itself (A → B → A)`,
		`schema/B.yaml:2: type B extends itself (B → A → B)`,
		`schema/E.yaml:5: field "x" is inherited from D (schema/D.yaml:5); a subtype adds fields, it does not declare its parent's again`,
		`schema/D.yaml:3: type D is abstract: it has no entities, so no id_prefix; give it to the types that extend it`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
