package diff

import "github.com/dynamatt/provenance/internal/compare"

// The comparison of values and texts is package compare's, which history
// shares (DES-0058); these are its names as diff's outputs and templates
// know them.
type (
	Op    = compare.Op
	Text  = compare.Text
	Block = compare.Block
	Word  = compare.Word
	Field = compare.Field
	Row   = compare.Row
)

const (
	Same    = compare.Same
	Added   = compare.Added
	Removed = compare.Removed
	Changed = compare.Changed
)

var (
	compareValues = compare.Values
	script        = compare.Script
	pairs         = compare.Pairs
	similar       = compare.Similar
)
