package load

import (
	"errors"

	"github.com/dynamatt/provenance/internal/export"
	"github.com/dynamatt/provenance/internal/history"
)

// History is the repository's git history, read through the sources (G7):
// HEAD and how the working tree differs from it, and, when asked, HEAD's
// content hash and the log of the entities each commit changed.
type History struct {
	// SHA is HEAD's full commit hash.
	SHA   string
	Dirty *history.Dirty

	root        string
	content     history.Content
	contentHash string
	log         *history.Log
}

// History reads the repository's git history the first time it is asked
// for; nil outside a git repository, and in one with no commits yet,
// which have none.
func (r *Result) History() (*History, error) {
	if !r.historyRead {
		r.history, r.historyErr = readHistory(r.Repo.Root, r.content)
		r.historyRead = true
	}
	return r.history, r.historyErr
}

func readHistory(root string, content history.Content) (*History, error) {
	repo, err := history.Open(root)
	if errors.Is(err, history.ErrNotRepository) || errors.Is(err, history.ErrNoCommits) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	h := &History{SHA: repo.HEAD(), root: root, content: content}
	if h.Dirty, err = repo.Status(content); err != nil {
		return nil, err
	}
	return h, nil
}

// ContentHash is the DHF content hash of HEAD, suffixed -dirty when the
// working tree differs from HEAD, read the first time it is asked for:
// it reads every committed content file, which fmt, needing only Dirty,
// doesn't.
func (h *History) ContentHash() (string, error) {
	if h.contentHash != "" {
		return h.contentHash, nil
	}
	repo, err := history.Open(h.root)
	if err != nil {
		return "", err
	}
	defer repo.Close()
	hash, err := repo.ContentHash(h.SHA)
	if err != nil {
		return "", err
	}
	if h.Dirty.Any() {
		hash += "-dirty"
	}
	h.contentHash = hash
	return hash, nil
}

// Log is the first-parent history from HEAD (DES-0023), read the first
// time it is asked for. Unlike the rest of History it needs every commit:
// a shallow clone that cut some off is a *history.ShallowError.
func (h *History) Log() (*history.Log, error) {
	if h.log != nil {
		return h.log, nil
	}
	repo, err := history.Open(h.root)
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	if h.log, err = repo.Log(h.content); err != nil {
		return nil, err
	}
	return h.log, nil
}

// Git is what an export stamps into its output (DES-0021, DES-0023); nil
// outside a git repository, when outputs carry no commit or content hash.
func (r *Result) Git() (*export.Git, error) {
	h, err := r.History()
	if err != nil || h == nil {
		return nil, err
	}
	log, err := h.Log()
	if err != nil {
		return nil, err
	}
	hash, err := h.ContentHash()
	if err != nil {
		return nil, err
	}
	return &export.Git{SHA: h.SHA, ContentHash: hash, Log: log, Dirty: h.Dirty}, nil
}
