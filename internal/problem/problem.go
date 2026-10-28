// Package problem is a problem with a repository's content, found while
// loading it or by an exporter's check, under the built-in check validate
// reports it under (DES-0048). Every problem has the same shape whoever
// finds it, so validate reports them all one way, and export stops at the
// first that stops it.
package problem

// Problem is one problem with a repository's content.
type Problem struct {
	// Check is the built-in check it is reported under, such as
	// "content/frontmatter". The set is open: a new problem names its own
	// check, and validate's catalogue (validate.Checks) lists them all.
	Check string
	// Warning is set for a problem reported without failing the gate
	// (REQ-0055); every other problem is an error.
	Warning bool
	// Path is slash-separated and relative to the repository root; Line is
	// 0 when not known; Entity is the ID of the entity it is about, or "".
	Path   string
	Line   int
	Entity string
	// Msg is the problem as validate reports it.
	Msg string
	// Err is what export stops at, worded as export has always worded it,
	// or nil for a problem export carries on past, such as a file that is
	// not an entity or a template nothing uses.
	Err error
}

// First is the Err of the first problem that stops an export, or nil.
func First(problems []Problem) error {
	for _, p := range problems {
		if p.Err != nil {
			return p.Err
		}
	}
	return nil
}
