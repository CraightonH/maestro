import type {
  Approval,
  CollectionResponse,
  ConfigBackupDetailResponse,
  ConfigBackupsResponse,
  ConfigRawResponse,
  ConfigValidateResponse,
  EventItem,
  Message,
  PackSaveRequest,
  PackSaveResponse,
  RetryEntry,
  RunsResponse,
  SourceSummary,
  StatusResponse,
} from "./types";

async function fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || `${path} ${response.status}`);
  }
  return payload as T;
}

export async function fetchDashboardData() {
  const [status, sources, runs, approvals, messages, retries, events, rawConfig, backups] =
    await Promise.all([
      fetchJSON<StatusResponse>("/api/v1/status"),
      fetchJSON<CollectionResponse<SourceSummary>>("/api/v1/sources"),
      fetchJSON<RunsResponse>("/api/v1/runs"),
      fetchJSON<CollectionResponse<Approval>>("/api/v1/approvals"),
      fetchJSON<CollectionResponse<Message>>("/api/v1/messages"),
      fetchJSON<CollectionResponse<RetryEntry>>("/api/v1/retries"),
      fetchJSON<CollectionResponse<EventItem>>("/api/v1/events"),
      fetchJSON<ConfigRawResponse>("/api/v1/config/raw").catch(() => ({
        generated_at: new Date().toISOString(),
        editable: false,
        yaml: "",
      })),
      fetchJSON<ConfigBackupsResponse>("/api/v1/config/backups").catch(() => ({
        generated_at: new Date().toISOString(),
        count: 0,
        items: [],
      })),
    ]);

  return { status, sources, runs, approvals, messages, retries, events, rawConfig, backups };
}

export function openStream(onUpdate: () => void, onError?: () => void) {
  const stream = new EventSource("/api/v1/stream");
  stream.addEventListener("update", () => {
    onUpdate();
  });
  stream.onerror = () => {
    onError?.();
  };
  return stream;
}

export async function resolveApproval(requestId: string, action: "approve" | "reject") {
  return fetchJSON<{ ok: boolean }>(`/api/v1/approvals/${encodeURIComponent(requestId)}/${action}`, {
    method: "POST",
  });
}

export async function replyToMessage(requestId: string, reply: string) {
  return fetchJSON<{ ok: boolean }>(`/api/v1/messages/${encodeURIComponent(requestId)}/reply`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ reply }),
  });
}

export async function validateConfig(yaml: string) {
  return fetchJSON<ConfigValidateResponse>("/api/v1/config/validate", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ yaml }),
  });
}

export async function dryRunConfig(yaml: string) {
  return fetchJSON<ConfigValidateResponse>("/api/v1/config/dry-run", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ yaml }),
  });
}

export async function saveConfig(yaml: string) {
  return fetchJSON<ConfigValidateResponse>("/api/v1/config/save", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ yaml }),
  });
}

export async function fetchBackupDetail(name: string) {
  return fetchJSON<ConfigBackupDetailResponse>(`/api/v1/config/backups/${encodeURIComponent(name)}`);
}

export async function createBackup() {
  return fetchJSON<{ ok: boolean }>("/api/v1/config/backups/create", {
    method: "POST",
  });
}

export async function restoreBackup(name: string) {
  return fetchJSON<{ ok: boolean; restart_needed?: boolean }>(`/api/v1/config/backups/${encodeURIComponent(name)}`, {
    method: "POST",
  });
}

export async function savePack(request: PackSaveRequest) {
  return fetchJSON<PackSaveResponse>("/api/v1/packs/save", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
}

export async function stopRun(runId: string) {
  return fetchJSON<{ ok: boolean; run: string; action: string }>(`/api/v1/runs/${encodeURIComponent(runId)}/stop`, {
    method: "POST",
  });
}
