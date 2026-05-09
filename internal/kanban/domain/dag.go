package domain

import "agentd/internal/models"

// ValidateDAG returns ErrCircularDependency when task dependencies contain a cycle.
func ValidateDAG(plan models.DraftPlan) error {
	inDegree, children := buildDependencyGraph(plan.Tasks)
	queue := initialReadyIDs(inDegree)
	if visitedCount(queue, inDegree, children) != len(plan.Tasks) {
		return models.ErrCircularDependency
	}
	return nil
}

func buildDependencyGraph(tasks []models.DraftTask) (map[string]int, map[string][]string) {
	inDegree := make(map[string]int, len(tasks))
	children := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		inDegree[task.ID()] = 0
	}
	for _, task := range tasks {
		addDependencyEdges(task, inDegree, children)
	}
	return inDegree, children
}

func addDependencyEdges(task models.DraftTask, inDegree map[string]int, children map[string][]string) {
	taskID := task.ID()
	for _, parent := range task.DependsOn {
		children[parent] = append(children[parent], taskID)
		inDegree[taskID]++
	}
}

func initialReadyIDs(inDegree map[string]int) []string {
	queue := make([]string, 0, len(inDegree))
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	return queue
}

func visitedCount(queue []string, inDegree map[string]int, children map[string][]string) int {
	visited := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited++
		queue = appendReadyChildren(queue, current, inDegree, children)
	}
	return visited
}

func appendReadyChildren(
	queue []string,
	current string,
	inDegree map[string]int,
	children map[string][]string,
) []string {
	for _, child := range children[current] {
		inDegree[child]--
		if inDegree[child] == 0 {
			queue = append(queue, child)
		}
	}
	return queue
}
