package kanban

import (
	"database/sql"
	"fmt"
	"path/filepath"
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
	db          *sql.DB
	projectsDir string
	canceller   models.TaskCanceller
}

var (
	_ models.KanbanStore        = (*Store)(nil)
	_ models.ScheduledTaskStore = (*Store)(nil)
)
var _ models.KanbanBoardContract = (*Store)(nil)

func NewStore(db *sql.DB, projectsDirs ...string) *Store {
	root := "/tmp/agentd-projects"
	if len(projectsDirs) > 0 {
		root = projectsDirs[0]
	}
	absolute, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		panic(fmt.Sprintf("resolve projects dir: %v", err))
	}
	return &Store{db: db, projectsDir: absolute}
}

// WithCanceller returns a shallow copy of the Store that invokes the given
// canceller after a human comment moves a task to IN_CONSIDERATION.
func (s *Store) WithCanceller(c models.TaskCanceller) *Store {
	cp := *s
	cp.canceller = c
	return &cp
}

func OpenStore(path string, projectsDirs ...string) (*Store, error) {
	db, err := Open(path, projectsDirs...)
	if err != nil {
		return nil, err
	}
	return NewStore(db, projectsDirs...), nil
}

func (s *Store) Close() error { return s.db.Close() }
