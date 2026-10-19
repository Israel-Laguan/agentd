package kanban

import "agentd/internal/models"

// WithTaskNotifier returns a shallow copy of the Store that calls n after every
// committed change to a task row, so live subscribers hear about it without each
// caller remembering to publish (B-074).
func (s *Store) WithTaskNotifier(n models.TaskChangeNotifier) *Store {
	cp := *s
	cp.notifier = n
	return &cp
}

// notifyTask announces a committed task change and passes the write's result
// through, so a write method can end with `return s.notifyTask(retryOnBusy(...))`.
// A failed write notifies nothing.
func (s *Store) notifyTask(task *models.Task, err error) (*models.Task, error) {
	if err == nil && task != nil {
		s.notify(*task)
	}
	return task, err
}

// notifyTasks is notifyTask for writes that change several rows.
func (s *Store) notifyTasks(tasks []models.Task, err error) ([]models.Task, error) {
	if err == nil {
		for _, task := range tasks {
			s.notify(task)
		}
	}
	return tasks, err
}

func (s *Store) notify(task models.Task) {
	if s.notifier != nil {
		s.notifier(task)
	}
}
