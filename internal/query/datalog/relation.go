package datalog

import (
	"fmt"
	"slices"
)

// Tuple is one fact's arguments.
type Tuple []Value

// Relation is a set of tuples of one arity, with indexes built on demand for
// the argument positions a join looks up by.
type Relation struct {
	Name   string
	Arity  int
	tuples []Tuple
	set    map[string]struct{}
	// indexes maps a bound-position mask to key → tuple positions.
	indexes map[uint64]map[string][]int
}

func newRelation(name string, arity int) *Relation {
	return &Relation{Name: name, Arity: arity, set: map[string]struct{}{}, indexes: map[uint64]map[string][]int{}}
}

// Len is the number of tuples.
func (r *Relation) Len() int { return len(r.tuples) }

// Contains reports whether t is in the relation.
func (r *Relation) Contains(t Tuple) bool {
	_, ok := r.set[string(tupleKey(nil, t))]
	return ok
}

// Tuples returns the tuples in a deterministic order, independent of the
// order they were derived in.
func (r *Relation) Tuples() []Tuple {
	out := slices.Clone(r.tuples)
	slices.SortFunc(out, compareTuples)
	return out
}

func compareTuples(a, b Tuple) int {
	for i := range a {
		if c := sortKey(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}

// add inserts t, reporting whether it was new.
func (r *Relation) add(t Tuple) bool {
	k := string(tupleKey(nil, t))
	if _, dup := r.set[k]; dup {
		return false
	}
	r.set[k] = struct{}{}
	r.tuples = append(r.tuples, t)
	i := len(r.tuples) - 1
	for mask, idx := range r.indexes {
		key := string(maskKey(nil, t, mask))
		idx[key] = append(idx[key], i)
	}
	return true
}

// lookup returns the positions of tuples whose masked positions equal key.
func (r *Relation) lookup(mask uint64, key []byte) []int {
	idx, ok := r.indexes[mask]
	if !ok {
		idx = map[string][]int{}
		for i, t := range r.tuples {
			k := string(maskKey(nil, t, mask))
			idx[k] = append(idx[k], i)
		}
		r.indexes[mask] = idx
	}
	return idx[string(key)]
}

func tupleKey(b []byte, t Tuple) []byte {
	for _, v := range t {
		b = appendKey(b, v)
	}
	return b
}

func maskKey(b []byte, t Tuple, mask uint64) []byte {
	for i, v := range t {
		if mask&(1<<i) != 0 {
			b = appendKey(b, v)
		}
	}
	return b
}

// Database holds relations by name. It is not safe for concurrent use,
// including concurrent Evals over the same database.
type Database struct {
	rels map[string]*Relation
}

func NewDatabase() *Database { return &Database{rels: map[string]*Relation{}} }

// Add inserts a fact. The first fact for a relation fixes its arity.
func (db *Database) Add(rel string, args ...Value) error {
	r, err := db.relation(rel, len(args))
	if err != nil {
		return err
	}
	for _, a := range args {
		if a.Kind == 0 {
			return fmt.Errorf("%s: fact has an invalid value", rel)
		}
	}
	r.add(Tuple(slices.Clone(args)))
	return nil
}

// MustAdd is Add for facts built by code whose arities are fixed.
func (db *Database) MustAdd(rel string, args ...Value) {
	if err := db.Add(rel, args...); err != nil {
		panic(err)
	}
}

// Relation returns the named relation, or nil.
func (db *Database) Relation(name string) *Relation { return db.rels[name] }

// Query returns the tuples of rel matching pattern, sorted; a zero Value in
// pattern matches anything.
func (db *Database) Query(rel string, pattern ...Value) []Tuple {
	r := db.rels[rel]
	if r == nil {
		return nil
	}
	var out []Tuple
	for _, t := range r.Tuples() {
		match := len(pattern) == len(t)
		for i := 0; match && i < len(pattern); i++ {
			match = pattern[i].Kind == 0 || pattern[i] == t[i]
		}
		if match {
			out = append(out, t)
		}
	}
	return out
}

func (db *Database) relation(name string, arity int) (*Relation, error) {
	r := db.rels[name]
	if r == nil {
		if arity > 64 {
			return nil, fmt.Errorf("%s: arity %d is above the limit of 64", name, arity)
		}
		r = newRelation(name, arity)
		db.rels[name] = r
	} else if r.Arity != arity {
		return nil, fmt.Errorf("%s: used with %d arguments and with %d", name, r.Arity, arity)
	}
	return r, nil
}

// overlay returns a database sharing db's relations, except those in
// derived, which are copied so evaluation can add to them. db's facts are
// never changed; a shared relation may gain indexes, which are only a cache.
func (db *Database) overlay(derived map[string]bool) *Database {
	out := NewDatabase()
	for name, r := range db.rels {
		if !derived[name] {
			out.rels[name] = r
			continue
		}
		c := newRelation(name, r.Arity)
		c.tuples = slices.Clone(r.tuples)
		for k := range r.set {
			c.set[k] = struct{}{}
		}
		out.rels[name] = c
	}
	return out
}
