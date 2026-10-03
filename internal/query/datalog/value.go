package datalog

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"
)

// Kind is a value's type. Values of different kinds never compare equal and
// are never ordered against each other.
type Kind uint8

const (
	Str  Kind = iota + 1 // text: strings, enums, dates (YYYY-MM-DD), IDs
	Num                  // number
	Bool                 // true or false
	Node                 // an opaque node name, e.g. a list row; never equal to a string
)

func (k Kind) String() string {
	switch k {
	case Str:
		return "string"
	case Num:
		return "number"
	case Bool:
		return "boolean"
	case Node:
		return "node"
	}
	return "invalid"
}

// Value is a constant. It is comparable, so it can be a map key.
type Value struct {
	Kind Kind
	S    string  // Str, Node
	N    float64 // Num
	B    bool    // Bool
}

func String(s string) Value    { return Value{Kind: Str, S: s} }
func Number(n float64) Value   { return Value{Kind: Num, N: n} }
func Boolean(b bool) Value     { return Value{Kind: Bool, B: b} }
func NodeValue(s string) Value { return Value{Kind: Node, S: s} }

func (v Value) String() string {
	switch v.Kind {
	case Str:
		return strconv.Quote(v.S)
	case Num:
		return strconv.FormatFloat(v.N, 'f', -1, 64)
	case Bool:
		return strconv.FormatBool(v.B)
	case Node:
		return "#" + v.S
	}
	return "?"
}

// CompareValues orders two values of the same kind: -1, 0 or 1. ok is false when
// the kinds differ (or are Node, which has no order beyond identity).
func CompareValues(a, b Value) (c int, ok bool) {
	if a.Kind != b.Kind {
		return 0, false
	}
	switch a.Kind {
	case Str:
		return strings.Compare(a.S, b.S), true
	case Num:
		switch {
		case a.N < b.N:
			return -1, true
		case a.N > b.N:
			return 1, true
		}
		return 0, true
	case Bool:
		switch {
		case a.B == b.B:
			return 0, true
		case !a.B:
			return -1, true
		}
		return 1, true
	}
	if a == b {
		return 0, true
	}
	return 0, false
}

// sortKey orders any two values totally (kind first), for deterministic
// output and aggregation order.
func sortKey(a, b Value) int {
	if a.Kind != b.Kind {
		return int(a.Kind) - int(b.Kind)
	}
	if a.Kind == Node {
		return strings.Compare(a.S, b.S)
	}
	c, _ := CompareValues(a, b)
	return c
}

// appendKey appends an injective encoding of v, used to key tuple sets and
// indexes.
func appendKey(b []byte, v Value) []byte {
	b = append(b, byte(v.Kind))
	switch v.Kind {
	case Str, Node:
		b = binary.AppendUvarint(b, uint64(len(v.S)))
		b = append(b, v.S...)
	case Num:
		n := v.N
		if n == 0 {
			n = 0 // -0 and +0 are the same number
		}
		b = binary.BigEndian.AppendUint64(b, math.Float64bits(n))
	case Bool:
		if v.B {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
	}
	return b
}
