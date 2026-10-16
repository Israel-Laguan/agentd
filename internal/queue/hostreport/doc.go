// Package hostreport turns the output files that host-introspection tasks
// collected (uname, hostnamectl, lscpu, free, lsblk, os-release) into a short
// summary. Values are parsed from those files by code, so a summary can never
// state a fact that no command produced, and machine or boot identifiers are
// never read, let alone written.
//
// It does not run commands, call a model, or decide when a task is a report
// task beyond ReportTarget; the worker owns scheduling and task state.
package hostreport
