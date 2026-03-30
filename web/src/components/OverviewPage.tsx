import type { Approval, ApprovalHistoryEntry, EventItem, Message, MessageHistoryEntry, MetricBreakdown, RetryEntry, RunMetrics } from "../types";
import { formatDate, formatRunMetrics } from "../lib/helpers";
import { EmptyState, PanelHeader, Pill } from "./ui";

export function OverviewPage({
  generatedAt,
  instanceMetrics,
  harnessMetrics,
  quickFilter,
  onQuickFilterChange,
  sourceGroup,
  onSourceGroupChange,
  sourceGroups,
  approvals,
  messages,
  retries,
  sources,
  events,
  approvalHistory,
  messageHistory,
  onOpenSource,
  onForcePollAll,
}: {
  generatedAt?: string;
  instanceMetrics?: RunMetrics;
  harnessMetrics?: MetricBreakdown[];
  quickFilter: "all" | "attention" | "awaiting-approval";
  onQuickFilterChange: (next: "all" | "attention" | "awaiting-approval") => void;
  sourceGroup: string;
  onSourceGroupChange: (next: string) => void;
  sourceGroups: string[];
  approvals: Approval[];
  messages: Message[];
  retries: RetryEntry[];
  sources: Array<{ name: string; displayGroup?: string; tracker: string; agentType?: string; health: string; visibleCount: number; metrics?: RunMetrics }>;
  events: EventItem[];
  approvalHistory: ApprovalHistoryEntry[];
  messageHistory: MessageHistoryEntry[];
  onOpenSource: (name: string) => void;
  onForcePollAll: () => void;
}) {
  return (
    <section className="page">
      <div className="overviewGrid">
        <section className="panel spanTwo">
          <PanelHeader
            title="Workflows"
            copy=""
            meta={`${sources.length} in view · ${formatDate(generatedAt)}`}
            actions={
              <div className="overviewToolbar">
                <div className="segmentedControl" role="tablist" aria-label="Workflow filter">
                  {[
                    { value: "all", label: "All" },
                    { value: "attention", label: "Attention" },
                    { value: "awaiting-approval", label: "Awaiting" },
                  ].map((option) => (
                    <button
                      key={option.value}
                      className={quickFilter === option.value ? "segmentButton active" : "segmentButton"}
                      onClick={() => onQuickFilterChange(option.value as "all" | "attention" | "awaiting-approval")}
                    >
                      {option.label}
                    </button>
                  ))}
                </div>
                <select className="compactSelect" value={sourceGroup} onChange={(event) => onSourceGroupChange(event.target.value)}>
                  <option value="">All groups</option>
                  {sourceGroups.map((group) => (
                    <option key={group} value={group}>
                      {group}
                    </option>
                  ))}
                </select>
                <button className="tinyButton primaryButton" onClick={onForcePollAll}>Poll all now</button>
              </div>
            }
          />
          {formatRunMetrics(instanceMetrics).length ? <p className="message">{formatRunMetrics(instanceMetrics).join(" · ")}</p> : null}
          <div className="stack">
            {sources.map((source) => (
              <button key={source.name} className="listCard" onClick={() => onOpenSource(source.name)}>
                <strong>{source.name}</strong>
                <span>{source.displayGroup || source.tracker} · {source.agentType || "unmapped"}</span>
                {formatRunMetrics(source.metrics).length ? <span>{formatRunMetrics(source.metrics).join(" · ")}</span> : null}
                <div className="pills">
                  <Pill tone={source.health === "active" ? "info" : source.health === "awaiting approval" || source.health === "retrying" ? "warn" : "ok"}>
                    {source.health}
                  </Pill>
                  <Pill>{source.visibleCount} visible</Pill>
                </div>
              </button>
            ))}
            {!sources.length ? <EmptyState copy="No workflows are configured in the current view." /> : null}
          </div>
        </section>

        <section className="panel">
          <PanelHeader title="Usage by harness" copy="" meta={`${harnessMetrics?.length || 0} harnesses`} />
          <div className="stack">
            {(harnessMetrics || []).map((item) => (
              <article key={item.name} className="listCard staticCard">
                <strong>{item.name}</strong>
                <span>{formatRunMetrics(item.metrics).join(" · ") || "No usage yet."}</span>
              </article>
            ))}
            {!harnessMetrics?.length ? <EmptyState copy="No harness metrics recorded yet." /> : null}
          </div>
        </section>

        <section className="panel">
          <PanelHeader title="Attention" copy="" meta={`${messages.length + approvals.length + retries.length} items`} />
          <div className="stack">
            {messages.map((message) => (
              <article key={message.request_id} className="listCard staticCard emphasisCard">
                <strong>{message.summary || "Operator gate"}</strong>
                <span>{message.issue_identifier || "Unknown issue"} · {message.agent_name || "operator control"}</span>
                <div className="pills">
                  <Pill tone="warn">{message.kind === "before_work" ? "before work" : "message"}</Pill>
                  <Pill>{formatDate(message.requested_at)}</Pill>
                </div>
              </article>
            ))}
            {approvals.map((approval) => (
              <article key={approval.request_id} className="listCard staticCard emphasisCard">
                <strong>{approval.tool_name || "Approval request"}</strong>
                <span>{approval.issue_identifier} · {approval.agent_name}</span>
                <div className="pills">
                  <Pill tone="warn">{approval.requested_at ? "waiting" : "pending"}</Pill>
                  <Pill>{approval.approval_policy || "manual"}</Pill>
                </div>
              </article>
            ))}
            {retries.map((retry) => (
              <article key={retry.issue_identifier} className="listCard staticCard">
                <strong>{retry.issue_identifier}</strong>
                <span>{retry.source_name}</span>
                <div className="pills">
                  <Pill tone="warn">attempt {retry.attempt}</Pill>
                </div>
              </article>
            ))}
            {!messages.length && !approvals.length && !retries.length ? <EmptyState copy="No operator controls, approvals, or retries need attention right now." /> : null}
          </div>
        </section>

        <section className="panel">
          <PanelHeader title="Recent operator decisions" copy="" meta={`${approvalHistory.length + messageHistory.length} shown`} />
          <div className="stack">
            {messageHistory.map((entry) => (
              <article key={`${entry.request_id}-${entry.replied_at || ""}`} className="listCard staticCard">
                <strong>{entry.issue_identifier || entry.run_id || "message"}</strong>
                <span>{entry.kind === "before_work" || entry.kind === "before_work_reply" ? "before work" : "message"} · {entry.reply || "replied"}</span>
                <div className="pills">
                  <Pill tone="info">{entry.outcome || "resolved"}</Pill>
                  <Pill>{entry.resolved_via || "maestro"}</Pill>
                  <Pill>{formatDate(entry.replied_at || entry.requested_at)}</Pill>
                </div>
              </article>
            ))}
            {approvalHistory.map((entry) => (
              <article key={`${entry.request_id}-${entry.decided_at || ""}`} className="listCard staticCard">
                <strong>{entry.issue_identifier || entry.run_id || "decision"}</strong>
                <span>{entry.tool_name || "tool"} · {entry.decision || "decision"}</span>
                <div className="pills">
                  <Pill tone={entry.outcome === "rejected" ? "warn" : "ok"}>{entry.outcome || "recorded"}</Pill>
                  <Pill>{formatDate(entry.decided_at || entry.requested_at)}</Pill>
                </div>
              </article>
            ))}
            {!approvalHistory.length && !messageHistory.length ? <EmptyState copy="No operator decision history in the current view." /> : null}
          </div>
        </section>

        <section className="panel fullSpan">
          <PanelHeader title="Recent activity" copy="" meta={`${events.length} events`} />
          <div className="timeline">
            {events.slice(0, 8).map((event, index) => (
              <article key={`${event.time}-${index}`} className="timelineItem">
                <strong>{event.message}</strong>
                <span>{event.level || "INFO"} · {formatDate(event.time)} · {event.source || "runtime"}{event.issue ? ` · ${event.issue}` : ""}</span>
              </article>
            ))}
            {!events.length ? <EmptyState copy="No events in the current view." /> : null}
          </div>
        </section>
      </div>
    </section>
  );
}
