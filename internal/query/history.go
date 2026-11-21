package query

import (
	"errors"

	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/model"
	"github.com/dynamatt/provenance/internal/query/datalog"
	"github.com/dynamatt/provenance/internal/schema"
)

// History reads the entities' changes from git (DES-0058). The graph calls
// it the first time a condition or formula names changes or last_changed,
// so a project that never does never reads its history.
type History func() (*history.EntityHistory, error)

// ErrNoHistory is reading changes where there is no git history to read
// them from.
var ErrNoHistory = errors.New("changes and last_changed read the repository's git history, and there is none here: run Provenance inside the git repository")

// NewGraphWith is NewGraph with the entities' history, read when first
// named; h nil when there is none.
func NewGraphWith(s *schema.Schema, entities []*model.Entity, h History) (*Graph, error) {
	g := newGraph(s, entities)
	g.history = h
	if err := g.derive(); err != nil {
		return nil, err
	}
	return g, nil
}

// useHistory adds every entity's changes to the facts, once (DES-0058):
// each change a row of changes, the newest also last_changed's one row.
func (g *Graph) useHistory() error {
	if g.historyRead {
		return g.historyErr
	}
	g.historyRead = true
	if g.history == nil {
		g.historyErr = ErrNoHistory
		return g.historyErr
	}
	eh, err := g.history()
	if err != nil {
		g.historyErr = err
		return err
	}
	if eh == nil {
		g.historyErr = ErrNoHistory
		return g.historyErr
	}
	order := make(map[string]int, len(eh.Order))
	for i, c := range eh.Order {
		order[c] = i
	}
	addHistoryFacts(g.facts, g.Entities, eh, order)
	return nil
}

func addHistoryFacts(db *datalog.Database, entities []*model.Entity, eh *history.EntityHistory, order map[string]int) {
	str := datalog.String
	row := func(id, list string, i int, c *history.Change) {
		node := RowNode(id, list, i)
		db.MustAdd(RelRow, str(id), str(list), node)
		if c.Commit != "" {
			// The working tree's change has no commit: it is the newest,
			// after every commit.
			db.MustAdd(RelField, node, str("commit"), datalog.CommitValue(c.Commit, order[c.Commit]))
		}
		for name, v := range map[string]string{"date": c.Date, "author": c.Author, "subject": c.Subject, "kind": c.Kind} {
			if v != "" {
				db.MustAdd(RelField, node, str(name), str(v))
			}
		}
		for j, f := range c.Fields {
			db.MustAdd(RelField, node, str("fields"), str(f))
			db.MustAdd(RelItem, node, str("fields"), datalog.Number(float64(j)), str(f))
		}
	}
	for _, e := range entities {
		changes := eh.Changes[e.ID]
		for i, c := range changes {
			row(e.ID, schema.Changes.Name, i, c)
		}
		if len(changes) > 0 {
			row(e.ID, schema.LastChanged.Name, 0, changes[0])
		}
	}
}
