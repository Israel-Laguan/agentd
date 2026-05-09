package main

import (
	"fmt"
	"io"

	"agentd/internal/kanban"
)

func closeStore(store *kanban.Store) {
	_ = store.Close()
}

func writeLine(w io.Writer, value string) error {
	_, err := fmt.Fprintln(w, value)
	return err
}

func writeFormat(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}
