import { useState } from "react";
import type { Approval, ConfigAgentSummary, ConfigSourceSummary, EventItem, Run, RunOutput } from "../types";
import { formatDate, sourceGroupHref, sourceProjectHref, sourceRepoHref, sourceScopeHref } from "../lib/helpers";
import { EmptyState, Metric, PanelHeader, Pill } from "./ui";

export function AgentWorkspace({
  agent,
  currentRun,
  currentOutput,
  approvals,
  events,
  sources,
  onResolveApproval,
}: {
  agent: ConfigAgentSummary;
  currentRun?: Run;
  currentOutput?: RunOutput;
  approvals: Approval[];
  events: EventItem[];
  sources: ConfigSourceSummary[];
  onResolveApproval: (requestId: string, action: "approve" | "reject") => Promise<void>;
}) {
  const [tab, setTab] = useState<"summary" | "logs" | "routing" | "prompt">("summary");

  return (
    <section className="page">
      <div className="agentGrid">
        <section className="panel spanTwo">
          <div className="panelHeader">
            <div>
              <h3>{agent.name}</h3>
              <p>{agent.description || "This is the runtime profile for an agent pack across every workflow that uses it."}</p>
            </div>
            <span>{agent.agent_pack || "custom pack"}</span>
          </div>
          <div className="heroLayout">
            <div className="heroLead">
              <span className="eyebrow">Runtime profile</span>
              <strong>{sources.length} workflow{sources.length === 1 ? "" : "s"}</strong>
              <p>{currentRun ? `One live workflow is using this pack right now: ${currentRun.source_name} on ${currentRun.issue.identifier || currentRun.id}.` : "This agent pack is reusable across workflows and does not own work directly."}</p>
              <div className="pills">
                <Pill tone="info">{agent.harness}</Pill>
                <Pill>{agent.workspace}</Pill>
                <Pill tone={agent.approval_policy === "manual" ? "warn" : "ok"}>{agent.approval_policy}</Pill>
              </div>
            </div>
            <div className="heroMetrics">
              <Metric label="Mapped workflows" value={String(sources.length)} />
              <Metric label="Approvals" value={approvals.length ? `${approvals.length} waiting` : "Clear"} />
              <Metric label="Live workflows" value={currentRun ? "1" : "0"} />
              <Metric label="Max concurrency" value={String(agent.max_concurrent)} />
            </div>
          </div>
        </section>

        <section className="panel spanTwo">
          <div className="pageTabs">
            <button className={tab === "summary" ? "tab active" : "tab"} onClick={() => setTab("summary")}>Summary</button>
            <button className={tab === "logs" ? "tab active" : "tab"} onClick={() => setTab("logs")}>Logs</button>
            <button className={tab === "routing" ? "tab active" : "tab"} onClick={() => setTab("routing")}>Routing</button>
            <button className={tab === "prompt" ? "tab active" : "tab"} onClick={() => setTab("prompt")}>Prompt</button>
          </div>

          {tab === "summary" ? (
            <div className="agentTabGrid">
              <section className="panel nestedPanel">
                <PanelHeader title="Live workflow sample" copy="One current run using this agent pack." meta={currentRun?.id || "Idle"} />
                {currentRun ? (
                  <>
                    <div className="detailList">
                      <Metric label="Workflow" value={currentRun.source_name || "n/a"} />
                      <Metric label="Issue" value={currentRun.issue.identifier || "n/a"} />
                      <Metric label="Tracker state" value={currentRun.issue.state || "n/a"} />
                      <Metric label="Started" value={formatDate(currentRun.started_at)} />
                      <Metric label="Attempt" value={String(currentRun.attempt)} />
                      <Metric label="Last activity" value={formatDate(currentRun.last_activity_at)} />
                    </div>
                    {currentRun.issue.url ? (
                      <div className="quickLinkRow">
                        <a className="quickLink" href={currentRun.issue.url} target="_blank" rel="noreferrer">
                          Open {currentRun.issue.identifier || "issue"}
                        </a>
                      </div>
                    ) : null}
                  </>
                ) : (
                  <EmptyState copy="This agent is not running work right now." />
                )}
              </section>

              <section className="panel nestedPanel">
                <PanelHeader title="Current approvals" copy="Approval requests currently tied to this agent." meta={`${approvals.length} waiting`} />
                <div className="stack">
                  {approvals.map((approval) => (
                    <article key={approval.request_id} className="listCard staticCard">
                      <strong>{approval.tool_name || "Approval request"}</strong>
                      <span>{approval.issue_identifier || "Unknown issue"}</span>
                      <div className="buttonRow">
                        <button className="actionButton primary" onClick={() => void onResolveApproval(approval.request_id, "approve")}>Approve</button>
                        <button className="actionButton" onClick={() => void onResolveApproval(approval.request_id, "reject")}>Reject</button>
                      </div>
                    </article>
                  ))}
                  {!approvals.length ? <EmptyState copy="No approval requests are waiting for this agent." /> : null}
                </div>
              </section>
            </div>
          ) : null}

          {tab === "routing" ? (
            <section className="panel nestedPanel">
              <PanelHeader title="Routing and filters" copy="The trackers, labels, assignee rules, and scope that feed this agent." meta={`${sources.length} sources`} />
              <div className="routeGrid">
                {sources.map((source) => (
                  <article key={source.name} className="detailCard">
                    <div className="detailCardHeader">
                      <div>
                        <strong>{source.name}</strong>
                        <span>{source.tracker}</span>
                      </div>
                      {sourceScopeHref(source) ? (
                        <a href={sourceScopeHref(source)} target="_blank" rel="noreferrer">
                          Open scope
                        </a>
                      ) : null}
                    </div>
                    <div className="detailList compact">
                      <Metric label="Project" value={source.project || "n/a"} />
                      <Metric label="Group" value={source.group || "n/a"} />
                      <Metric label="Repo" value={source.repo || "n/a"} />
                      <Metric label="Assignee" value={source.assignee || "n/a"} />
                    </div>
                    <div className="quickLinkRow">
                      {sourceProjectHref(source) ? <a className="quickLink" href={sourceProjectHref(source)} target="_blank" rel="noreferrer">Project</a> : null}
                      {sourceGroupHref(source) ? <a className="quickLink" href={sourceGroupHref(source)} target="_blank" rel="noreferrer">Group</a> : null}
                      {sourceRepoHref(source) ? <a className="quickLink" href={sourceRepoHref(source)} target="_blank" rel="noreferrer">Repo</a> : null}
                      {sourceScopeHref(source) ? <a className="quickLink" href={sourceScopeHref(source)} target="_blank" rel="noreferrer">Scope</a> : null}
                    </div>
                    <div className="pills">
                      {(source.filter_labels || []).map((label) => <Pill key={label}>{label}</Pill>)}
                      {(source.epic_filter_iids || []).map((iid) => <Pill key={iid} tone="info">epic #{iid}</Pill>)}
                      {(source.epic_filter_labels || []).map((label) => <Pill key={label} tone="info">epic:{label}</Pill>)}
                      {(source.issue_filter_labels || []).map((label) => <Pill key={label} tone="warn">issue:{label}</Pill>)}
                    </div>
                  </article>
                ))}
              </div>
            </section>
          ) : null}

          {tab === "logs" ? (
            <div className="agentTabGrid">
              <section className="panel nestedPanel">
                <PanelHeader title="Live output" copy="Tail of stdout and stderr for the selected agent’s current run." meta={currentOutput?.updated_at ? formatDate(currentOutput.updated_at) : "No output"} />
                <pre className="logBlock">{currentOutput?.stdout_tail || currentOutput?.stderr_tail || "No captured output yet."}</pre>
              </section>
              <section className="panel nestedPanel">
                <PanelHeader title="Agent timeline" copy="Recent activity scoped to this agent and its current run." meta={`${events.length} events`} />
                <div className="timeline">
                  {events.slice(0, 8).map((event, index) => (
                    <article key={`${event.time}-${index}`} className="timelineItem">
                      <strong>{event.message}</strong>
                      <span>{event.level || "INFO"} · {formatDate(event.time)} · {event.source || "runtime"}{event.issue ? ` · ${event.issue}` : ""}</span>
                    </article>
                  ))}
                  {!events.length ? <EmptyState copy="No events for the selected agent yet." /> : null}
                </div>
              </section>
            </div>
          ) : null}

          {tab === "prompt" ? (
            <section className="panel nestedPanel">
              <PanelHeader title="Prompt contract" copy="System prompt, prompt template, tools, skills, and context files loaded for this agent." meta={agent.prompt} />
              <div className="promptGrid">
                <article className="detailCard">
                  <span className="eyebrow">Capabilities</span>
                  <div className="pills">
                    {(agent.tools || []).map((tool) => <Pill key={tool} tone="info">{tool}</Pill>)}
                    {(agent.skills || []).map((skill) => <Pill key={skill}>{skill}</Pill>)}
                    {(agent.env_keys || []).map((envKey) => <Pill key={envKey}>{envKey}</Pill>)}
                  </div>
                </article>
                <article className="detailCard">
                  <span className="eyebrow">System prompt</span>
                  <pre className="logBlock">{agent.system_prompt || "No system prompt found."}</pre>
                </article>
                <article className="detailCard spanTwo">
                  <span className="eyebrow">Prompt template</span>
                  <pre className="logBlock">{agent.prompt_body || "No prompt body found."}</pre>
                </article>
                {(agent.context_bodies || []).map((context) => (
                  <article key={context.path} className="detailCard spanTwo">
                    <span className="eyebrow">{context.path}</span>
                    <pre className="logBlock">{context.content || ""}</pre>
                  </article>
                ))}
              </div>
            </section>
          ) : null}
        </section>
      </div>
    </section>
  );
}
