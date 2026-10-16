package models

// HostReportTaskTitle is the title the planner gives the final task of a
// host-introspection plan. The host report renderer recognizes it, so the
// report is rendered from collected files instead of being written by the
// model (B-045, B-053).
const HostReportTaskTitle = "Build system report"
