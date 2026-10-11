package yamlnode

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestLookup(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("a: 1\nb: [x]\n"), &doc); err != nil {
		t.Fatal(err)
	}
	m := doc.Content[0]
	if v := Lookup(m, "a"); v == nil || v.Value != "1" {
		t.Errorf("a: %v", v)
	}
	if Lookup(m, "c") != nil || Lookup(nil, "a") != nil || Lookup(Lookup(m, "b"), "x") != nil {
		t.Error("a missing key, nil node or non-mapping gave a value")
	}
}

func TestErrorLine(t *testing.T) {
	var v struct{ A int }
	for _, tc := range []struct {
		src  string
		line int
		msg  string
	}{
		// Found at the end of the text, the line after it.
		{"a: [\n", 2, "did not find expected node content"},
		{"x: 1\na: text\nb: 2\n", 2, "cannot construct !!str `text` into int"},
		// A construct begun on an earlier line is named with its line.
		{"x: [1,\n  2\ny: 3\n", 3, "did not find expected ',' or ']' (while parsing a flow sequence at line 1)"},
	} {
		err := yaml.Unmarshal([]byte(tc.src), &v)
		if err == nil {
			t.Fatalf("%q: no error", tc.src)
		}
		if line, msg := ErrorLine(err); line != tc.line || msg != tc.msg {
			t.Errorf("%q: got %d %q, want %d %q", tc.src, line, msg, tc.line, tc.msg)
		}
	}
}

func TestUnmarshalCountsFileLines(t *testing.T) {
	var doc yaml.Node
	if err := Unmarshal([]byte("a: 1\nb: 2\n"), 2, &doc); err != nil {
		t.Fatal(err)
	}
	if b := Lookup(doc.Content[0], "b"); b == nil || b.Line != 3 {
		t.Errorf("b = %+v, want line 3", b)
	}
	for _, tc := range []struct {
		src   string
		first int
		line  int
		msg   string
	}{
		{"a: b: c\n", 2, 2, "mapping values are not allowed in this context"},
		{"x: 1\na: b: c\n", 1, 2, "mapping values are not allowed in this context"},
		// An error found at the end of the text is put on its last line.
		{"a: [b\n", 1, 1, "did not find expected ',' or ']'"},
		{"x: 1\ny: [b\n", 5, 6, "did not find expected ',' or ']'"},
		{"x: [b,\n  c\ny: 1\n", 5, 7, "did not find expected ',' or ']' (while parsing a flow sequence at line 5)"},
	} {
		err := Unmarshal([]byte(tc.src), tc.first, &yaml.Node{})
		if line, msg := ErrorLine(err); line != tc.line || msg != tc.msg {
			t.Errorf("%q from line %d: got %d %q, want %d %q", tc.src, tc.first, line, msg, tc.line, tc.msg)
		}
	}
}
