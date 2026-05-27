package kanban

import (
	"database/sql"
	"strings"

	"agentd/internal/models"
)

const defaultAgentID = "default"

// resolveAgentID returns draft.AgentID when explicitly set, otherwise the
// store default. This lets materialize callers pre-assign a task to a
// specific agent profile, eliminating the race between dispatch and a
// separate assign call.
func resolveAgentID(draftAgentID string) string {
	if id := strings.TrimSpace(draftAgentID); id != "" {
		return id
	}
	return defaultAgentID
}

type Store struct {
	db        *sql.DB
	canceller models.TaskCanceller
}

var (
	_ models.KanbanStore         = (*Store)(nil)
	_ models.ScheduledTaskStore = (*Store)(nil)
)
var _ models.KanbanBoardContract = (*Store)(nil)

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// WithCanceller returns a shallow copy of the Store that invokes the given
// canceller after a human comment moves a task to IN_CONSIDERATION.
func (s *Store) WithCanceller(c models.TaskCanceller) *Store {
	cp := *s
	cp.canceller = c
	return &cp
}

func OpenStore(path string) (*Store, error) {
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	return NewStore(db), nil
}

func (s *Store) Close() error { return s.db.Close() }
