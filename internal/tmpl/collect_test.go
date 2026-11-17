package tmpl

import (
	"strings"
	"testing"
)

func TestCollect(t *testing.T) {
	v1 := Data{"ID": "VER-2", "tested_by": []Data{{"ID": "EVD-3"}, {"ID": "EVD-1"}}}
	v2 := Data{"ID": "VER-1", "tested_by": []Data{{"ID": "EVD-1"}}}
	r1 := Data{"ID": "REQ-1", "verified_by": []Data{v1, v2}, "parent": nil}
	r2 := Data{"ID": "REQ-2", "verified_by": []Data{v1}, "parent": r1}
	ids := func(ds []Data) string {
		var out []string
		for _, d := range ds {
			out = append(out, d["ID"].(string))
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		list   any
		fields []string
		want   string
	}{
		{[]Data{r1, r2}, []string{"verified_by"}, "VER-1 VER-2"},
		{[]Data{r1, r2}, []string{"verified_by", "tested_by"}, "EVD-1 EVD-3"},
		{[]Data{r2, r1}, []string{"parent"}, "REQ-1"},
		{r2, []string{"parent", "verified_by"}, "VER-1 VER-2"},
		{[]Data{r2, r1}, nil, "REQ-1 REQ-2"},
		{nil, []string{"parent"}, ""},
	} {
		got, err := Collect(tc.list, tc.fields...)
		if err != nil || ids(got) != tc.want {
			t.Errorf("collect %v: got %q, %v; want %q", tc.fields, ids(got), err, tc.want)
		}
	}
	if _, err := Collect([]Data{r1}, "colour"); err == nil || err.Error() != `collect: REQ-1 has no field "colour"` {
		t.Errorf("unknown field: %v", err)
	}
	if _, err := Collect([]Data{{"ID": "R", "status": "x"}}, "status"); err == nil || !strings.Contains(err.Error(), "status is not a link") {
		t.Errorf("not a link: %v", err)
	}
}
