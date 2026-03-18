export type ViewMode = "overview" | "agent" | "workflow" | "settings" | "packs";

export interface Issue {
  id?: string;
  identifier?: string;
  title?: string;
  description?: string;
  url?: string;
  state?: string;
  labels?: string[];
  updated_at?: string;
}

export interface RunOutput {
  run_id: string;
  source_name: string;
  issue_identifier?: string;
  stdout_tail?: string;
  stderr_tail?: string;
  updated_at?: string;
}

export interface Run {
  id: string;
  agent_name: string;
  agent_type?: string;
  issue: Issue;
  source_name: string;
  harness_kind?: string;
  workspace_path?: string;
  status: string;
  attempt: number;
  approval_policy?: string;
  approval_state?: string;
  started_at?: string;
  last_activity_at?: string;
  completed_at?: string;
  error?: string;
  output?: RunOutput;
}

export interface Approval {
  request_id: string;
  run_id?: string;
  issue_id?: string;
  issue_identifier?: string;
  agent_name?: string;
  tool_name?: string;
  tool_input?: string;
  approval_policy?: string;
  requested_at?: string;
  resolvable: boolean;
}

export interface Message {
  request_id: string;
  run_id?: string;
  issue_id?: string;
  issue_identifier?: string;
  source_name?: string;
  agent_name?: string;
  kind?: string;
  summary?: string;
  body?: string;
  requested_at?: string;
  resolvable: boolean;
}

export interface ApprovalHistoryEntry {
  request_id: string;
  run_id?: string;
  issue_id?: string;
  issue_identifier?: string;
  agent_name?: string;
  tool_name?: string;
  approval_policy?: string;
  decision?: string;
  reason?: string;
  requested_at?: string;
  decided_at?: string;
  outcome?: string;
}

export interface MessageHistoryEntry {
  request_id: string;
  run_id?: string;
  issue_id?: string;
  issue_identifier?: string;
  source_name?: string;
  agent_name?: string;
  kind?: string;
  summary?: string;
  body?: string;
  reply?: string;
  resolved_via?: string;
  requested_at?: string;
  replied_at?: string;
  outcome?: string;
}

export interface RetryEntry {
  issue_id?: string;
  issue_identifier: string;
  source_name: string;
  attempt: number;
  due_at?: string;
  error?: string;
}

export interface EventItem {
  time?: string;
  level?: string;
  source?: string;
  run_id?: string;
  issue?: string;
  message: string;
}

export interface SourceSummary {
  name: string;
  display_group?: string;
  tags?: string[];
  tracker: string;
  last_poll_at?: string;
  last_poll_count: number;
  claimed_count: number;
  retry_count: number;
  active_run_count: number;
  pending_approvals: number;
  pending_messages: number;
}

export interface ConfigFileSummary {
  path: string;
  content?: string;
}

export interface ConfigSourceSummary {
  name: string;
  display_group?: string;
  tags?: string[];
  tracker: string;
  agent_type?: string;
  base_url?: string;
  project?: string;
  group?: string;
  repo?: string;
  filter_labels?: string[];
  filter_iids?: number[];
  filter_states?: string[];
  assignee?: string;
  epic_filter_labels?: string[];
  epic_filter_iids?: number[];
  issue_filter_labels?: string[];
  issue_filter_iids?: number[];
  issue_states?: string[];
  poll_interval: string;
  token_env?: string;
}

export interface ConfigAgentSummary {
  description?: string;
  name: string;
  instance_name?: string;
  agent_pack?: string;
  pack_path?: string;
  harness: string;
  workspace: string;
  approval_policy: string;
  max_concurrent: number;
  prompt: string;
  prompt_body?: string;
  system_prompt?: string;
  context_files?: string[];
  context_bodies?: ConfigFileSummary[];
  tools?: string[];
  skills?: string[];
  env_keys?: string[];
}

export interface PackSaveRequest {
  original_name?: string;
  name: string;
  description?: string;
  instance_name?: string;
  harness: string;
  workspace: string;
  approval_policy: string;
  max_concurrent: number;
  tools?: string[];
  skills?: string[];
  env_keys?: string[];
  prompt_body?: string;
  context_body?: string;
}

export interface PackSaveResponse {
  ok: boolean;
  generated_at: string;
  restart_needed: boolean;
  validation_error?: string;
}

export interface ConfigSummary {
  config_path: string;
  agent_packs_dir?: string;
  user_name?: string;
  gitlab_username?: string;
  linear_username?: string;
  workspace_root: string;
  state_dir: string;
  log_dir: string;
  log_max_files: number;
  max_concurrent_global: number;
  stall_timeout?: string;
  default_poll_interval?: string;
  hooks: {
    after_create?: string;
    before_run?: string;
    after_run?: string;
    before_remove?: string;
    timeout?: string;
  };
  controls: {
    before_work_enabled: boolean;
    before_work_prompt?: string;
  };
  sources: ConfigSourceSummary[];
  agents: ConfigAgentSummary[];
}

export interface StatusResponse {
  generated_at: string;
  config: ConfigSummary;
  snapshot: {
    approval_history?: ApprovalHistoryEntry[];
    message_history?: MessageHistoryEntry[];
  };
}

export interface CollectionResponse<T> {
  generated_at: string;
  count: number;
  items: T[];
}

export interface RunsResponse extends CollectionResponse<Run> {
  outputs: RunOutput[];
}

export interface ConfigRawResponse {
  generated_at: string;
  config_path?: string;
  editable: boolean;
  yaml?: string;
}

export interface ConfigValidateResponse {
  ok: boolean;
  generated_at: string;
  config_path?: string;
  editable: boolean;
  restart_needed: boolean;
  validation_error?: string;
  diff?: string;
  config?: ConfigSummary;
}

export interface ConfigBackupSummary {
  name: string;
  path: string;
  created_at: string;
}

export interface ConfigBackupsResponse extends CollectionResponse<ConfigBackupSummary> {
  config_path?: string;
}

export interface ConfigBackupDetailResponse {
  generated_at: string;
  config_path?: string;
  backup?: ConfigBackupSummary;
  yaml?: string;
  diff?: string;
}

export interface StreamUpdate {
  generated_at: string;
  snapshot: {
    source_count: number;
    active_run_count: number;
    retry_count: number;
    approval_count: number;
    recent_event_count: number;
  };
}
