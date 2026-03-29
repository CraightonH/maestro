import { useMemo, useState } from "react";
import type { Approval, ConfigSourceSummary, EventItem, Message, MessageHistoryEntry, RetryEntry, Run, RunOutput, SourceSummary } from "../types";
import { formatRunMetrics, formatTrackerRateLimit, sourceScopeHref, type SourceDraft } from "../lib/helpers";
import { Control, EmptyState, PanelHeader, Pill } from "./ui";

export function WorkflowWorkspace({
  workflow,
  runtime,
  currentRun,
  currentOutput,
  runs,
  retries,
  approvals,
  messages,
  messageHistory,
  events,
  sourceDraft,
  showEditor,
  onSourceDraftChange,
  onAppendSourceDraft,
  onOpenYaml,
  onLoadSelectedWorkflow,
  onNewWorkflow,
  onToggleEditor,
  onStopWorkflow,
  onForcePollWorkflow,
  onOpenAgent,
  onResolveMessage,
}: {
  workflow?: ConfigSourceSummary;
  runtime?: SourceSummary;
  currentRun?: Run;
  currentOutput?: RunOutput;
  runs: Run[];
  retries: RetryEntry[];
  approvals: Approval[];
  messages: Message[];
  messageHistory: MessageHistoryEntry[];
  events: EventItem[];
  sourceDraft: SourceDraft;
  showEditor: boolean;
  onSourceDraftChange: (draft: SourceDraft) => void;
  onAppendSourceDraft: () => void;
  onOpenYaml: () => void;
  onLoadSelectedWorkflow: () => void;
  onNewWorkflow: () => void;
  onToggleEditor: () => void;
  onStopWorkflow: () => void;
  onForcePollWorkflow: () => void;
  onOpenAgent: (name: string) => void;
  onResolveMessage: (requestId: string, reply: string) => Promise<void>;
}) {
  const [expandedLogs, setExpandedLogs] = useState(false);
  const [messageReplies, setMessageReplies] = useState<Record<string, string>>({});
  const agentName = workflow?.agent_type || "";
  const metricsSummary = formatRunMetrics(currentRun?.metrics);
  const logText = useMemo(() => {
    const merged = [currentOutput?.stdout_tail, currentOutput?.stderr_tail].filter(Boolean).join("\n");
    if (!merged) return "No captured output yet.";
    const lines = merged.trim().split("\n");
    if (expandedLogs || lines.length <= 8) return lines.join("\n");
    return lines.slice(-8).join("\n");
  }, [currentOutput?.stderr_tail, currentOutput?.stdout_tail, expandedLogs]);

  if (!workflow) {
    return (
      <section className="page">
        <section className="panel">
          <EmptyState copy="Select a workflow from the sidebar to inspect or edit it." />
        </section>
      </section>
    );
  }

  const trackerHref = sourceScopeHref(workflow);

  return (
    <section className="page">
      <div className="settingsGrid workflowGrid">
        <section className="panel spanTwo">
          <div className="workflowHeaderCompact">
            <div className="workflowTaskBlock">
              <span className="eyebrow">Current task</span>
              {currentRun?.issue.url ? (
                <a className="workflowTaskLink" href={currentRun.issue.url} target="_blank" rel="noreferrer">
                  {currentRun.issue.identifier || "Idle"}
                </a>
              ) : (
                <strong>{currentRun?.issue.identifier || "Idle"}</strong>
              )}
              <p>{currentRun?.issue.title || "This workflow is currently idle. When it picks up work, the active issue will appear here."}</p>
              {metricsSummary.length ? <p className="message">{metricsSummary.join(" · ")}</p> : null}
              <div className="pills">
                <Pill tone={workflowStatus(runtime) === "active" ? "info" : workflowStatus(runtime) === "awaiting approval" || workflowStatus(runtime) === "retrying" ? "warn" : "ok"}>
                  {workflowStatus(runtime)}
                </Pill>
                {agentName ? (
                  <button className="pillButton" onClick={() => onOpenAgent(agentName)}>
                    {agentName}
                  </button>
                ) : (
                  <Pill>unmapped</Pill>
                )}
                {(workflow.tags || []).map((tag) => <Pill key={tag}>{tag}</Pill>)}
              </div>
            </div>
            <div className="workflowSideRail">
              <div className="workflowTopBar">
                <div className="inlineActions">
                  <button className="tinyButton" onClick={showEditor ? onToggleEditor : onLoadSelectedWorkflow}>
                    {showEditor ? "Hide editor" : "Edit workflow"}
                  </button>
                  {trackerHref ? (
                    <a className="tinyButton linkButton" href={trackerHref} target="_blank" rel="noreferrer">
                      Open tracker
                    </a>
                  ) : null}
                  <button className="tinyButton primaryButton" onClick={onForcePollWorkflow}>Poll now</button>
                  {agentName ? <button className="tinyButton" onClick={() => onOpenAgent(agentName)}>Open agent</button> : null}
                  {currentRun ? <button className="tinyButton primaryButton" onClick={onStopWorkflow}>Stop workflow</button> : null}
                </div>
              </div>
              <div className="workflowMetaRow">
                <CompactMeta label="Tracker" value={workflow.tracker} />
                <CompactMeta label="Poll" value={workflow.poll_interval} />
                <CompactMeta label="Assignee" value={workflow.assignee || "n/a"} />
                <CompactMeta label="Labels" value={(workflow.filter_labels || []).join(", ") || "n/a"} />
                <CompactMeta label="Issue labels" value={(workflow.issue_filter_labels || []).join(", ") || "n/a"} />
                <CompactMeta label="Rate limit" value={formatTrackerRateLimit(runtime?.rate_limit)} />
                <CompactMeta label="Queue" value={`${messages.length} controls · ${approvals.length} approvals · ${retries.length} retries`} />
              </div>
            </div>
          </div>
        </section>

        <section className="panel spanTwo">
          <PanelHeader
            title="Live output"
            copy=""
            meta={currentOutput?.updated_at || "No output"}
            actions={
              currentOutput ? (
                <button className="tinyButton" onClick={() => setExpandedLogs((value) => !value)}>
                  {expandedLogs ? "Collapse logs" : "Expand logs"}
                </button>
              ) : null
            }
          />
          <pre className="logBlock">{logText}</pre>
        </section>

        <section className="panel spanTwo">
          <PanelHeader title="Controls and conversation" copy="" meta={`${messages.length} waiting · ${messageHistory.length} recent`} />
          <div className="conversationGrid">
            <div className="stack">
              {messages.map((message) => (
                <article key={message.request_id} className="detailCard inlineMessageCard">
                  <div className="detailCardHeader">
                    <div>
                      <strong>{message.summary || "Operator control"}</strong>
                      <span>{messageKindLabel(message.kind)} · {message.issue_identifier || workflow.name}</span>
                    </div>
                    <Pill tone="warn">{message.requested_at ? "waiting" : "pending"}</Pill>
                  </div>
                  <p className="message">{message.body || "Reply to continue."}</p>
                  <div className="messageComposer">
                    <input
                      value={messageReplies[message.request_id] || ""}
                      onChange={(event) =>
                        setMessageReplies((current) => ({
                          ...current,
                          [message.request_id]: event.target.value,
                        }))
                      }
                      placeholder={message.kind === "before_work_reply" ? "Reply with the answer" : "Reply with start or operator guidance"}
                    />
                    <div className="buttonRow">
                      {message.kind === "before_work" || message.kind === "before_work_review" ? (
                        <button className="actionButton primary" onClick={() => void onResolveMessage(message.request_id, "start")}>
                          Start
                        </button>
                      ) : null}
                      <button
                        className="actionButton"
                        onClick={() => void onResolveMessage(message.request_id, messageReplies[message.request_id] || "")}
                      >
                        Send reply
                      </button>
                    </div>
                  </div>
                </article>
              ))}
              {!messages.length ? <EmptyState copy="No pending control messages for this workflow." /> : null}
            </div>
            <div className="stack">
              {messageHistory.map((entry) => (
                <article key={`${entry.request_id}-${entry.replied_at || entry.requested_at || ""}`} className="detailCard inlineHistoryCard">
                  <div className="detailCardHeader">
                    <div>
                      <strong>{messageKindLabel(entry.kind)}</strong>
                      <span>{entry.issue_identifier || workflow.name}</span>
                    </div>
                    <Pill tone={entry.outcome === "resolved" ? "info" : "warn"}>{entry.resolved_via || "maestro"}</Pill>
                  </div>
                  {entry.summary ? <p className="message">{entry.summary}</p> : null}
                  {entry.reply ? <pre className="miniLogBlock">{entry.reply}</pre> : null}
                  <div className="pills">
                    <Pill>{entry.outcome || "recorded"}</Pill>
                    <Pill>{entry.replied_at || entry.requested_at || "unknown time"}</Pill>
                  </div>
                </article>
              ))}
              {!messageHistory.length ? <EmptyState copy="No recent control history for this workflow yet." /> : null}
            </div>
          </div>
        </section>

        <section className="panel">
          <PanelHeader title="Pending work" copy="" meta={`${runs.length + approvals.length + retries.length} items`} />
          <div className="stack">
            {currentRun ? (
              <article className="listCard staticCard">
                <strong>Active: {currentRun.issue.identifier || currentRun.id}</strong>
                <span>{currentRun.status} · attempt {currentRun.attempt}</span>
              </article>
            ) : null}
            {messages.map((message) => (
              <article key={message.request_id} className="listCard staticCard">
                <strong>{message.summary || "Operator control"}</strong>
                <span>{message.kind === "before_work" ? "before work gate" : "message"} · {message.issue_identifier || "unknown issue"}</span>
              </article>
            ))}
            {approvals.map((approval) => (
              <article key={approval.request_id} className="listCard staticCard">
                <strong>Approval: {approval.tool_name || "request"}</strong>
                <span>{approval.issue_identifier || "unknown issue"}</span>
              </article>
            ))}
            {retries.map((retry) => (
              <article key={`${retry.source_name}-${retry.issue_identifier}`} className="listCard staticCard">
                <strong>Retry: {retry.issue_identifier}</strong>
                <span>retry attempt {retry.attempt}</span>
              </article>
            ))}
            {!currentRun && !messages.length && !approvals.length && !retries.length ? <EmptyState copy="No active task, control requests, approvals, or retries for this workflow." /> : null}
          </div>
        </section>

        <section className="panel spanTwo">
          <PanelHeader title="Recent activity" copy="" meta={`${events.length} events`} />
          <div className="timeline">
            {events.slice(0, 10).map((event, index) => (
              <article key={`${event.time}-${index}`} className="timelineItem">
                <strong>{event.message}</strong>
                <span>{event.issue || workflow.name}</span>
              </article>
            ))}
            {!events.length ? <EmptyState copy="No recent events for this workflow." /> : null}
          </div>
        </section>

        {showEditor ? (
          <section className="panel spanTwo" id="source-draft">
            <PanelHeader
              title="Workflow editor"
              copy="Create a new workflow or revise the selected one, then validate and stage the YAML update."
              meta={sourceDraft.tracker}
              actions={
                <div className="inlineActions">
                  <button className="tinyButton" onClick={onNewWorkflow}>New workflow</button>
                  <button className="tinyButton primaryButton" onClick={onAppendSourceDraft}>Preview and stage</button>
                  <button className="tinyButton" onClick={onOpenYaml}>Open YAML</button>
                </div>
              }
            />
            <div className="builderGrid">
              <Control label="Tracker">
                <select value={sourceDraft.tracker} onChange={(event) => onSourceDraftChange({ ...sourceDraft, tracker: event.target.value as SourceDraft["tracker"] })}>
                  <option value="gitlab">gitlab</option>
                  <option value="gitlab-epic">gitlab-epic</option>
                  <option value="linear">linear</option>
                </select>
              </Control>
              <Control label="Name">
                <input value={sourceDraft.name} onChange={(event) => onSourceDraftChange({ ...sourceDraft, name: event.target.value })} />
              </Control>
              <Control label="Agent type">
                <input value={sourceDraft.agentType} onChange={(event) => onSourceDraftChange({ ...sourceDraft, agentType: event.target.value })} />
              </Control>
              <Control label="Project">
                <input value={sourceDraft.project} onChange={(event) => onSourceDraftChange({ ...sourceDraft, project: event.target.value })} />
              </Control>
              <Control label="Group">
                <input value={sourceDraft.group} onChange={(event) => onSourceDraftChange({ ...sourceDraft, group: event.target.value })} />
              </Control>
              <Control label="Repo">
                <input value={sourceDraft.repo} onChange={(event) => onSourceDraftChange({ ...sourceDraft, repo: event.target.value })} />
              </Control>
              <Control label="Filter labels">
                <input value={sourceDraft.labels} onChange={(event) => onSourceDraftChange({ ...sourceDraft, labels: event.target.value })} />
              </Control>
              <Control label="Assignee">
                <input value={sourceDraft.assignee} onChange={(event) => onSourceDraftChange({ ...sourceDraft, assignee: event.target.value })} />
              </Control>
              <Control label="Epic IIDs">
                <input value={sourceDraft.epicIids} onChange={(event) => onSourceDraftChange({ ...sourceDraft, epicIids: event.target.value })} />
              </Control>
              <Control label="Issue labels">
                <input value={sourceDraft.issueLabels} onChange={(event) => onSourceDraftChange({ ...sourceDraft, issueLabels: event.target.value })} />
              </Control>
            </div>
          </section>
        ) : null}
      </div>
    </section>
  );
}

function CompactMeta({ label, value }: { label: string; value: string }) {
  return (
    <div className="compactMeta">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function workflowStatus(runtime?: SourceSummary) {
  if (!runtime) return "idle";
  if (runtime.pending_approvals > 0) return "awaiting approval";
  if (runtime.retry_count > 0) return "retrying";
  if (runtime.active_run_count > 0) return "active";
  return "idle";
}

function messageKindLabel(kind?: string) {
  switch (kind) {
    case "before_work":
    case "before_work_review":
      return "Before work gate";
    case "before_work_reply":
      return "Before work question";
    case "agent_message":
      return "Agent message";
    default:
      return kind || "Operator control";
  }
}
