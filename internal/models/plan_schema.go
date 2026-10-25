package models

import "strings"

// draftPlanSchemaHint is the JSON contract for DraftPlan, shown to the model
// in the planning prompt and repeated in every self-correction message so
// retries converge instead of guessing field names.
const draftPlanSchemaHint = `The JSON object must have exactly this shape:
{
  "project_name": string (required, short kebab-case name),
  "description": string (optional),
  "source_path": string (optional, local source directory to copy into the workspace),
  "start_empty_workspace": boolean (optional, start with an empty workspace),
  "tasks": [ (required, at least one)
    {
      "ref_id": string (optional, unique id other tasks can reference),
      "title": string (required),
      "description": string (what the worker must do),
      "assignee": "SYSTEM" (agentd runs it) or "HUMAN" (a person must do it),
      "depends_on": [string] (optional, ref_id values of earlier tasks),
      "success_criteria": [string] (optional),
      "requires_workspace_change": boolean (optional, true when the task must create or change files or run commands in the workspace)
    }
  ]
}
Example:
{"project_name":"hello-cli","tasks":[{"ref_id":"t1","title":"Write main.go","description":"Create a Go program that prints hello.","assignee":"SYSTEM","success_criteria":["go run . prints hello"]},{"ref_id":"t2","title":"Add README","description":"Document how to run it.","assignee":"SYSTEM","depends_on":["t1"]}]}`

// SchemaHint returns the DraftPlan JSON schema plus an example, used by the
// gateway to build prompts and corrective retry messages.
func (DraftPlan) SchemaHint() string {
	const intro = "Respond with a single JSON object and no prose or markdown fences. "
	hint := intro + draftPlanSchemaHint
	return strings.TrimSpace(hint)
}
