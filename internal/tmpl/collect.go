package tmpl

import (
	"fmt"
	"slices"
	"strings"
)

// Collect follows each named field from every entity in list, in turn, and
// returns the entities reached, each once, ordered by ID (DES-0030):
// {{range collect .scope "verified_by" "tested_by"}} is every execution of
// every protocol verifying a requirement in scope. A field may be a link,
// an incoming facet or a query field; list may be one entity or a list of
// them. A name the entities don't have is an error.
func Collect(list any, fields ...string) ([]Data, error) {
	cur, err := entities(list, "collect")
	if err != nil {
		return nil, err
	}
	for _, name := range fields {
		var next []Data
		for _, e := range cur {
			v, ok := e[name]
			if !ok {
				return nil, fmt.Errorf("collect: %s has no field %q", e["ID"], name)
			}
			reached, err := entities(v, "collect")
			if err != nil {
				return nil, fmt.Errorf("collect: %s's %s is not a link: %w", e["ID"], name, err)
			}
			next = append(next, reached...)
		}
		cur = next
	}
	out := []Data{}
	for _, e := range cur {
		if !slices.ContainsFunc(out, func(o Data) bool { return o["ID"] == e["ID"] }) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b Data) int {
		x, _ := a["ID"].(string)
		y, _ := b["ID"].(string)
		return strings.Compare(x, y)
	})
	return out, nil
}

// entities reads v as entities: nil, one, or a list of them.
func entities(v any, fn string) ([]Data, error) {
	switch v := v.(type) {
	case nil:
		return nil, nil
	case Data:
		return []Data{v}, nil
	case []Data:
		return v, nil
	}
	return nil, fmt.Errorf("%s: want an entity or a list of entities, got %T", fn, v)
}
