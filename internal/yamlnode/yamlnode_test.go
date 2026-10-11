package yamlnode

import (
	"testing"

	"go.yaml.in/yaml/v3"
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
		{"a: [\n", 1, "did not find expected node content"},
		{"x: 1\na: text\nb: 2\n", 2, "cannot unmarshal !!str `text` into int"},
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
	}{
		// yaml leaves the line out of this error on the first line it reads.
		{"a: b: c\n", 2, 2},
		{"x: 1\na: b: c\n", 1, 2},
		// yaml places an unclosed flow sequence's error a line before it; one
		// on the text's first line is kept on that line.
		{"a: [b\n", 1, 1},
	} {
		err := Unmarshal([]byte(tc.src), tc.first, &yaml.Node{})
		if line, _ := ErrorLine(err); line != tc.line {
			t.Errorf("%q from line %d: error %v at line %d, want %d", tc.src, tc.first, err, line, tc.line)
		}
	}
}
