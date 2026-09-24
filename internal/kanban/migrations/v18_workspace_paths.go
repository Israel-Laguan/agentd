package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
)

const systemProjectID = "00000000-0000-0000-0000-000000000001"

type projectPath struct {
	id      string
	updated string
}

// v18RunMode selects what a run over the projects table does: repair
// malformed workspace paths and/or advance the stored schema version.
type v18RunMode struct {
	repair     bool
	setVersion bool
}

var (
	v18MigrateMode  = v18RunMode{repair: true, setVersion: true}
	v18ValidateMode = v18RunMode{repair: false, setVersion: false}
)

func migrateWorkspacePaths(ctx context.Context, db *sql.DB, projectsDir string, mode v18RunMode) error {
	root, err := workspaceRoot(projectsDir)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate workspace paths: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var projectsTable int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'projects'`).Scan(&projectsTable); err != nil {
		return fmt.Errorf("migrate workspace paths: inspect projects table: %w", err)
	}
	if projectsTable == 0 {
		if mode.setVersion {
			if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, '18', datetime('now')) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, schemaVersionKey); err != nil {
				return fmt.Errorf("set schema version: %w", err)
			}
		}
		return tx.Commit()
	}

	updated, err := collectProjectPaths(ctx, tx, root, mode.repair)
	if err != nil {
		return err
	}
	if err := updateProjectPaths(ctx, tx, updated); err != nil {
		return err
	}
	if mode.setVersion {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES (?, '18', datetime('now'))
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			schemaVersionKey); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate workspace paths: commit transaction: %w", err)
	}
	return nil
}

func workspaceRoot(projectsDir string) (string, error) {
	if strings.TrimSpace(projectsDir) == "" {
		return "", fmt.Errorf("migrate workspace paths: projects_dir must not be empty")
	}
	root, err := filepath.Abs(filepath.Clean(projectsDir))
	if err != nil {
		return "", fmt.Errorf("migrate workspace paths: resolve projects_dir: %w", err)
	}
	if root == filepath.Clean(string(filepath.Separator)) {
		return "", fmt.Errorf("migrate workspace paths: projects_dir must not be filesystem root")
	}
	return root, nil
}

func collectProjectPaths(ctx context.Context, tx *sql.Tx, root string, repair bool) ([]projectPath, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name, workspace_path FROM projects ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("migrate workspace paths: list projects: %w", err)
	}
	updated := make([]projectPath, 0)
	used := make(map[string]string)
	for rows.Next() {
		var id, name, workspace string
		if err := rows.Scan(&id, &name, &workspace); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("migrate workspace paths: scan project: %w", err)
		}
		path, ok, err := projectWorkspace(root, id, name, workspace, repair)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if other, exists := used[path]; exists {
			return nil, fmt.Errorf("migrate workspace paths: projects %q and %q share workspace path %q", other, id, path)
		}
		used[path] = id
		updated = append(updated, projectPath{id: id, updated: path})
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("migrate workspace paths: close projects: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate workspace paths: iterate projects: %w", err)
	}
	return updated, nil
}

func projectWorkspace(root, id, name, workspace string, repair bool) (string, bool, error) {
	if id == systemProjectID || name == "_system" {
		if id != systemProjectID || workspace != "_system" {
			return "", false, fmt.Errorf("migrate workspace paths: system project %q has invalid id/path %q", id, workspace)
		}
		return "", false, nil
	}
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", false, fmt.Errorf("migrate workspace paths: project %q is not a safe path component", id)
	}
	var canonical string
	if workspace == id && repair {
		canonical = filepath.Join(root, id)
	} else if !filepath.IsAbs(workspace) {
		return "", false, fmt.Errorf("migrate workspace paths: project %q has invalid relative workspace path %q", id, workspace)
	} else {
		canonical = filepath.Clean(workspace)
	}
	if err := validateWorkspaceContainment(root, id, workspace, canonical); err != nil {
		return "", false, err
	}
	return canonical, true, nil
}

func validateWorkspaceContainment(root, id, workspace, canonical string) error {
	if canonical == root {
		return fmt.Errorf("migrate workspace paths: project %q workspace path equals projects root", id)
	}
	rel, err := filepath.Rel(root, canonical)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("migrate workspace paths: project %q workspace path %q is outside projects root %q", id, workspace, root)
	}
	return nil
}

func updateProjectPaths(ctx context.Context, tx *sql.Tx, paths []projectPath) error {
	for _, project := range paths {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET workspace_path = ? WHERE id = ?`, project.updated, project.id); err != nil {
			return fmt.Errorf("migrate workspace paths: update project %q: %w", project.id, err)
		}
	}
	return nil
}
