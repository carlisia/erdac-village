package crawl

import (
	"context"
	"strings"
	"sync"

	"github.com/carlisia/erdac-village/internal/store"
)

// MemoryStore keeps a run's results in memory and forgets them when it ends.
//
// It is the test double the store's consumers declare, and the store behind a
// sample run, where nothing is kept and nothing is searched. Nothing here
// survives the program, so it can never be mistaken for the knowledge base.
type MemoryStore struct {
	mu sync.Mutex
	// existing is what a run finds already held when it starts.
	existing []store.AddressState
	// Pages is everything written this run, by address.
	Pages  map[string]MemoryPage
	nextID int64
}

// MemoryPage is one stored candidate with everything that arrived with it.
type MemoryPage struct {
	ID      int64
	Page    store.Page
	Chunks  []store.Chunk
	Vectors [][]float32
}

// NewMemoryStore starts with what a previous run would have left behind.
func NewMemoryStore(existing []store.AddressState) *MemoryStore {
	return &MemoryStore{existing: existing, Pages: map[string]MemoryPage{}, nextID: 1}
}

func (m *MemoryStore) AddressStates(context.Context) ([]store.AddressState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.AddressState, len(m.existing))
	copy(out, m.existing)
	return out, nil
}

func (m *MemoryStore) StoreCandidate(_ context.Context, _ string, p store.Page, chunks []store.Chunk, vectors [][]float32) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.nextID
	if known, ok := m.Pages[p.URL]; ok {
		id = known.ID
	} else {
		m.nextID++
	}
	m.Pages[p.URL] = MemoryPage{ID: id, Page: p, Chunks: chunks, Vectors: vectors}
	return id, nil
}

// Held is what a later run would find already on file: every page written
// this run, as a candidate, carrying whatever judgement the address had.
func (m *MemoryStore) Held() []store.AddressState {
	m.mu.Lock()
	defer m.mu.Unlock()
	judgement := map[string]bool{}
	for _, a := range m.existing {
		judgement[a.URL] = a.Included
	}
	var out []store.AddressState
	for u, mp := range m.Pages {
		included, known := judgement[u]
		if !known {
			included = true
		}
		out = append(out, store.AddressState{
			URL:      u,
			Included: included,
			Candidate: &store.PageSummary{
				ID: mp.ID, Title: mp.Page.Title, ContentHash: mp.Page.ContentHash,
				LastMod: mp.Page.LastMod, FetchedAt: mp.Page.FetchedAt,
			},
		})
	}
	return out
}

// Markdown is every stored page's text in one string, for tests that assert
// what did and did not reach the store.
func (m *MemoryStore) Markdown() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var parts []string
	for _, mp := range m.Pages {
		parts = append(parts, mp.Page.Markdown)
	}
	return strings.Join(parts, "\n")
}

// VectorCount is how many vectors were stored across every page.
func (m *MemoryStore) VectorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, mp := range m.Pages {
		n += len(mp.Vectors)
	}
	return n
}
