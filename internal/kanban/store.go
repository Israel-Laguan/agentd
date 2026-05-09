package kanban

import (
	"database/sql"

	"agentd/internal/models"
)

const defaultAgentID = "default"

type Store struct {
	db        *sql.DB
	canceller models.TaskCanceller
}

var _ models.KanbanStore = (*Store)(nil)
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
