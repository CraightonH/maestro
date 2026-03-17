import type { ConfigAgentSummary, ViewMode } from "../types";
import { formatDate, viewHeading, viewTitle } from "../lib/helpers";

export function WorkspaceHeader({
  view,
  selectedAgent,
  workflowName,
  generatedAt,
  theme,
  onOpenHome,
  onRefresh,
  onToggleTheme,
}: {
  view: ViewMode;
  selectedAgent?: ConfigAgentSummary;
  workflowName?: string;
  generatedAt?: string;
  theme: "dark" | "light";
  onOpenHome: () => void;
  onRefresh: () => void;
  onToggleTheme: () => void;
}) {
  return (
    <header className="workspaceHeader">
      <div>
        <div className="workspaceBreadcrumb">
          <span className="crumbIcon">◫</span>
          <button className="crumbButton" onClick={onOpenHome}>Dashboard</button>
          <span className="crumbDivider">/</span>
          <span>{viewTitle(view)}</span>
        </div>
        <h2>{view === "workflow" ? workflowName || "Workflow" : viewHeading(view, selectedAgent)}</h2>
      </div>
      <div className="workspaceActions">
        <span className="workspaceMeta">Updated {formatDate(generatedAt)}</span>
        <button className="iconButton" onClick={onRefresh} aria-label="Refresh">
          ↻
        </button>
        <button className="iconButton" onClick={onToggleTheme} aria-label="Toggle theme">
          {theme === "dark" ? "☼" : "◐"}
        </button>
      </div>
    </header>
  );
}
