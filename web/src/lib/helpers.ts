import type {
  Approval,
  ConfigAgentSummary,
  ConfigSourceSummary,
  ExecutionSummary,
  Run,
  RunMetrics,
  SourceSummary,
  TrackerRateLimit,
  ViewMode,
} from "../types";

export type RouteState = {
  view: ViewMode;
  settingsTab: "general" | "yaml" | "backups";
  agentName: string;
  workflowName: string;
};

export type SourceDraftTracker = "gitlab" | "gitlab-epic" | "linear";

export type SourceDraft = {
  tracker: SourceDraftTracker;
  name: string;
  agentType: string;
  project: string;
  group: string;
  repo: string;
  labels: string;
  assignee: string;
  epicIids: string;
  issueLabels: string;
};

export type PackDraft = {
  originalName: string;
  name: string;
  description: string;
  instanceName: string;
  harness: string;
  workspace: string;
  approvalPolicy: string;
  maxConcurrent: string;
  tools: string;
  skills: string;
  envKeys: string;
  promptBody: string;
  contextBody: string;
};

export const emptySourceDraft: SourceDraft = {
  tracker: "gitlab",
  name: "",
  agentType: "",
  project: "",
  group: "",
  repo: "",
  labels: "",
  assignee: "",
  epicIids: "",
  issueLabels: "",
};

export const emptyPackDraft: PackDraft = {
  originalName: "",
  name: "",
  description: "",
  instanceName: "",
  harness: "claude-code",
  workspace: "git-clone",
  approvalPolicy: "manual",
  maxConcurrent: "1",
  tools: "",
  skills: "",
  envKeys: "",
  promptBody: "",
  contextBody: "",
};

export function sourceHealth(source?: SourceSummary) {
  if (!source) return "idle";
  if (source.pending_approvals > 0) return "awaiting approval";
  if (source.retry_count > 0) return "retrying";
  if (source.active_run_count > 0) return "active";
  return "idle";
}

export function attentionScore(run: Run) {
  let score = 0;
  if (run.approval_state === "awaiting" || run.status === "awaiting_approval") score += 10;
  if (run.error) score += 6;
  if (run.status === "active") score += 2;
  return score;
}

export function matchesSearch(query: string, parts: Array<string | undefined>) {
  return parts.some((part) => String(part || "").toLowerCase().includes(query));
}

export function formatDate(value?: string) {
  if (!value) return "n/a";
  return new Intl.DateTimeFormat(undefined, {
    month: "numeric",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

export function relativeTime(value?: string) {
  if (!value) return "n/a";
  const delta = Date.now() - new Date(value).getTime();
  const future = delta < 0;
  const minutes = Math.round(Math.abs(delta) / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return future ? `in ${minutes}m` : `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return future ? `in ${hours}h` : `${hours}h ago`;
  const days = Math.round(hours / 24);
  return future ? `in ${days}d` : `${days}d ago`;
}

export function formatRunMetrics(metrics?: RunMetrics) {
  if (!metrics) return [];
  const parts: string[] = [];
  if (typeof metrics.tokens_in === "number") parts.push(`${formatInteger(metrics.tokens_in)} in`);
  if (typeof metrics.tokens_out === "number") parts.push(`${formatInteger(metrics.tokens_out)} out`);
  if (typeof metrics.total_tokens === "number") parts.push(`${formatInteger(metrics.total_tokens)} total`);
  if (typeof metrics.cost_usd === "number") parts.push(formatCurrency(metrics.cost_usd));
  if (typeof metrics.duration_ms === "number") parts.push(formatDuration(metrics.duration_ms));
  if (typeof metrics.throughput_tokens_per_second === "number") parts.push(`${metrics.throughput_tokens_per_second.toFixed(1)} tok/s`);
  return parts;
}

export function formatExecutionSummary(execution?: ExecutionSummary) {
  if (!execution) return "";
  if (!execution.mode || execution.mode === "host") return "host";
  const parts: string[] = [execution.mode];
  if (execution.image) parts.push(`image=${execution.image}`);
  if (execution.network) parts.push(`network=${execution.network}`);
  if (typeof execution.cpus === "number" && execution.cpus > 0) parts.push(`cpus=${execution.cpus}`);
  if (execution.memory) parts.push(`memory=${execution.memory}`);
  if (typeof execution.pids_limit === "number" && execution.pids_limit > 0) parts.push(`pids=${execution.pids_limit}`);
  if (execution.auth_source) parts.push(`auth=${execution.auth_source}`);
  return parts.join(" · ");
}

export function formatTrackerRateLimit(rateLimit?: TrackerRateLimit) {
  if (!rateLimit) return "n/a";
  const parts: string[] = [];
  if (typeof rateLimit.remaining === "number" && typeof rateLimit.limit === "number") {
    parts.push(`${formatInteger(rateLimit.remaining)}/${formatInteger(rateLimit.limit)} left`);
  } else if (typeof rateLimit.limit === "number") {
    parts.push(`limit ${formatInteger(rateLimit.limit)}`);
  }
  if (rateLimit.reset_at) {
    parts.push(`resets ${relativeTime(rateLimit.reset_at)}`);
  }
  if (typeof rateLimit.retry_after_seconds === "number") {
    parts.push(`retry ${rateLimit.retry_after_seconds}s`);
  }
  return parts.join(" · ") || "n/a";
}

export function formatInteger(value: number) {
  return new Intl.NumberFormat().format(value);
}

export function formatCurrency(value: number) {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: value >= 1 ? 2 : 4,
    maximumFractionDigits: value >= 1 ? 2 : 4,
  }).format(value);
}

export function formatDuration(durationMs: number) {
  if (durationMs < 1000) return `${durationMs}ms`;
  const seconds = durationMs / 1000;
  if (seconds < 60) return `${seconds.toFixed(seconds >= 10 ? 0 : 1)}s`;
  const minutes = Math.floor(seconds / 60);
  const remainder = Math.round(seconds % 60);
  return `${minutes}m ${remainder}s`;
}

export function sourceScopeHref(source: ConfigSourceSummary) {
  if (!source.base_url) return "";
  if (source.tracker === "gitlab" && source.project) {
    return `${source.base_url.replace(/\/$/, "")}/${source.project}`;
  }
  if (source.tracker === "gitlab-epic" && source.group) {
    return `${source.base_url.replace(/\/$/, "")}/groups/${source.group}`;
  }
  if (source.tracker === "linear" && source.project) {
    return `${source.base_url.replace(/\/$/, "")}/project/${source.project}`;
  }
  if (source.repo) return source.repo;
  return "";
}

export function sourceEpicHrefs(source: ConfigSourceSummary) {
  if (source.tracker !== "gitlab-epic" || !source.base_url || !source.group) return [];
  const base = source.base_url.replace(/\/$/, "");
  return (source.epic_filter_iids || []).map((iid) => ({
    label: `Epic #${iid}`,
    href: `${base}/groups/${source.group}/-/epics/${iid}`,
  }));
}

export function sourceProjectHref(source: ConfigSourceSummary) {
  if (!source.base_url || !source.project) return "";
  const base = source.base_url.replace(/\/$/, "");
  if (source.tracker === "linear") {
    return `${base}/project/${source.project}`;
  }
  return `${base}/${source.project}`;
}

export function sourceGroupHref(source: ConfigSourceSummary) {
  if (!source.base_url || !source.group) return "";
  return `${source.base_url.replace(/\/$/, "")}/groups/${source.group}`;
}

export function sourceRepoHref(source: ConfigSourceSummary) {
  if (!source.repo) return "";
  if (/^https?:\/\//.test(source.repo)) return source.repo;
  if (!source.base_url) return source.repo;
  return `${source.base_url.replace(/\/$/, "")}/${source.repo.replace(/^\//, "")}`;
}

export function renderSourceDraft(draft: SourceDraft) {
  const lines = [
    "- name: " + (draft.name || "new-source"),
    "  tracker: " + draft.tracker,
    "  agent_type: " + (draft.agentType || "code-pr"),
  ];

  if (draft.project || draft.group) {
    lines.push("  connection:");
    if (draft.project) lines.push("    project: " + draft.project);
    if (draft.group && draft.tracker === "gitlab-epic") lines.push("    group: " + draft.group);
  }
  if (draft.repo) lines.push("  repo: " + draft.repo);
  if (draft.labels || draft.assignee) {
    lines.push("  filter:");
    if (draft.labels) {
      lines.push("    labels:");
      csvLines(draft.labels).forEach((label) => lines.push(`      - ${label}`));
    }
    if (draft.assignee) lines.push("    assignee: " + draft.assignee);
  }
  if (draft.tracker === "gitlab-epic") {
    const iids = csvLines(draft.epicIids);
    const issueLabels = csvLines(draft.issueLabels);
    if (iids.length) {
      lines.push("  epic_filter:", "    iids:");
      iids.forEach((iid) => lines.push(`      - ${iid}`));
    }
    if (issueLabels.length) {
      lines.push("  issue_filter:", "    labels:");
      issueLabels.forEach((label) => lines.push(`      - ${label}`));
    }
  }

  return lines.join("\n");
}

export function renderPackDraft(draft: PackDraft) {
  const lines = [
    "name: " + (draft.name || "new-pack"),
    "description: " + (draft.description || "purpose for this pack"),
    "instance_name: " + (draft.instanceName || draft.name || "new-pack"),
    "harness: " + draft.harness,
    "workspace: " + draft.workspace,
    "approval_policy: " + draft.approvalPolicy,
    "max_concurrent: " + (draft.maxConcurrent || "1"),
    "prompt: prompt.md",
    "tools:",
    ...csvLines(draft.tools).map((tool) => `  - ${tool}`),
    "skills:",
    ...csvLines(draft.skills).map((skill) => `  - ${skill}`),
  ];

  const envKeys = csvLines(draft.envKeys);
  if (envKeys.length) {
    lines.push("env:");
    envKeys.forEach((envKey) => lines.push(`  ${envKey}: ${envKey}`));
  }

  lines.push("context_files:", "  - context.md");
  return lines.join("\n");
}

export function csvLines(value: string) {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function viewTitle(view: ViewMode) {
  if (view === "agent") return "Agent";
  if (view === "workflow") return "Workflow";
  if (view === "settings") return "Settings";
  if (view === "packs") return "Agent Packs";
  return "Overview";
}

export function viewHeading(view: ViewMode, agent?: ConfigAgentSummary) {
  if (view === "agent") return agent?.name || "Agent workspace";
  if (view === "workflow") return "Workflow";
  if (view === "packs") return "Agent Packs";
  if (view === "settings") return "Settings";
  return "Overview";
}

export function viewSubtitle(view: ViewMode, agent?: ConfigAgentSummary) {
  if (view === "agent") {
    return agent?.description || "Runtime, routing, approvals, prompt contract, and current logs.";
  }
  if (view === "workflow") {
    return "A configured workstream that routes one source of work into a specific agent.";
  }
  if (view === "packs") {
    return "Inspect, revise, and persist reusable agent packs with prompts, tools, and context files.";
  }
  if (view === "settings") {
    return "Edit runtime defaults, validate YAML changes, and manage config backups safely.";
  }
  return "See the fleet first, then jump into the one agent or source that matters.";
}

export function latestApprovalText(approval?: Approval) {
  if (!approval) return "No approvals are waiting right now.";
  return `${approval.tool_name || "Approval request"} on ${approval.issue_identifier || "unknown issue"} · requested ${relativeTime(approval.requested_at)}`;
}

export function parseRoute(pathname: string): RouteState {
  const clean = pathname.replace(/\/+$/, "") || "/";
  if (clean === "/packs") {
    return { view: "packs", settingsTab: "general", agentName: "", workflowName: "" };
  }
  if (clean === "/settings" || clean === "/settings/general") {
    return { view: "settings", settingsTab: "general", agentName: "", workflowName: "" };
  }
  if (clean === "/settings/yaml") {
    return { view: "settings", settingsTab: "yaml", agentName: "", workflowName: "" };
  }
  if (clean === "/settings/backups") {
    return { view: "settings", settingsTab: "backups", agentName: "", workflowName: "" };
  }
  if (clean.startsWith("/workflows/")) {
    return {
      view: "workflow",
      settingsTab: "general",
      agentName: "",
      workflowName: decodeURIComponent(clean.slice("/workflows/".length)),
    };
  }
  if (clean.startsWith("/agents/")) {
    return {
      view: "agent",
      settingsTab: "general",
      agentName: decodeURIComponent(clean.slice("/agents/".length)),
      workflowName: "",
    };
  }
  return { view: "overview", settingsTab: "general", agentName: "", workflowName: "" };
}

export function routePath(view: ViewMode, settingsTab: "general" | "yaml" | "backups", agentName?: string, workflowName?: string) {
  if (view === "packs") return "/packs";
  if (view === "settings") {
    if (settingsTab === "yaml") return "/settings/yaml";
    if (settingsTab === "backups") return "/settings/backups";
    return "/settings";
  }
  if (view === "workflow" && workflowName) {
    return `/workflows/${encodeURIComponent(workflowName)}`;
  }
  if (view === "agent" && agentName) {
    return `/agents/${encodeURIComponent(agentName)}`;
  }
  return "/";
}
