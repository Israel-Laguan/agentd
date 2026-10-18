import type { BoardIssues } from "@/app/hooks/use-dashboard";

const BANNER = "mx-4 mt-3 px-3 py-2 rounded-md text-xs border";
const ERROR = `${BANNER} bg-red-500/10 border-red-500/30 text-red-400`;

// BoardNotices says when the board is not the whole truth: the daemon is
// unreachable, a project's tasks failed to load (its cards are the last known
// ones), or a project has more tasks than the board fetches.
export function BoardNotices({ unreachable, issues }: { unreachable: boolean; issues: BoardIssues }) {
  return (
    <>
      {unreachable && (
        <div role="alert" className={ERROR}>
          Could not reach daemon — board may be stale. Retrying&hellip;
        </div>
      )}
      {issues.failedProjects.map((f) => (
        <div key={f.projectId} role="alert" className={ERROR}>
          Could not load tasks for project {f.projectId}: {f.message}. Showing its last known tasks.
        </div>
      ))}
      {issues.cappedProjects.length > 0 && (
        <div role="status" className={`${BANNER} bg-yellow-500/10 border-yellow-500/30 text-yellow-400`}>
          Some tasks are not shown: project {issues.cappedProjects.join(", ")} has more tasks than the board loads.
        </div>
      )}
    </>
  );
}
