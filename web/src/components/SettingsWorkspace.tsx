import type { ConfigBackupDetailResponse, ConfigBackupSummary, ConfigRawResponse, ConfigSummary } from "../types";
import { Control, Metric, PanelHeader } from "./ui";

export type GeneralSettingsDraft = {
  maxConcurrentGlobal: string;
  defaultPollInterval: string;
  stallTimeout: string;
  logMaxFiles: string;
};

export function SettingsWorkspace({
  config,
  settingsTab,
  onChangeSettingsTab,
  rawConfig,
  configEditor,
  onConfigEditorChange,
  onValidate,
  onDryRun,
  onSave,
  configResult,
  configDiff,
  backups,
  selectedBackupName,
  selectedBackup,
  onSelectBackup,
  onCreateBackup,
  onRestoreBackup,
  settingsDraft,
  onSettingsDraftChange,
  onApplySettingsDraft,
  onOpenYaml,
}: {
  config: ConfigSummary;
  settingsTab: "general" | "yaml" | "backups";
  onChangeSettingsTab: (tab: "general" | "yaml" | "backups") => void;
  rawConfig?: ConfigRawResponse;
  configEditor: string;
  onConfigEditorChange: (value: string) => void;
  onValidate: () => void;
  onDryRun: () => void;
  onSave: () => void;
  configResult: string;
  configDiff: string;
  backups: ConfigBackupSummary[];
  selectedBackupName: string;
  selectedBackup: ConfigBackupDetailResponse | null;
  onSelectBackup: (backup: ConfigBackupSummary) => void;
  onCreateBackup: () => void;
  onRestoreBackup: () => void;
  settingsDraft: GeneralSettingsDraft;
  onSettingsDraftChange: (draft: GeneralSettingsDraft) => void;
  onApplySettingsDraft: () => void;
  onOpenYaml: () => void;
}) {
  return (
    <section className="page">
      <div className="pageTabs">
        <button className={settingsTab === "general" ? "tab active" : "tab"} onClick={() => onChangeSettingsTab("general")}>General</button>
        <button className={settingsTab === "yaml" ? "tab active" : "tab"} onClick={() => onChangeSettingsTab("yaml")}>YAML</button>
        <button className={settingsTab === "backups" ? "tab active" : "tab"} onClick={() => onChangeSettingsTab("backups")}>Backups</button>
      </div>

      {settingsTab === "general" ? (
        <div className="settingsGrid">
          <section className="panel spanTwo">
            <PanelHeader
              title="Global settings"
              copy="Edit the core runtime settings here, then apply them into YAML before saving."
              meta={config.user_name || "No operator name"}
              actions={
                <div className="inlineActions">
                  <button className="tinyButton primaryButton" onClick={onApplySettingsDraft}>Apply to YAML</button>
                  <button className="tinyButton" onClick={onOpenYaml}>Open YAML</button>
                </div>
              }
            />
            <div className="builderGrid">
              <Control label="Global concurrency">
                <input value={settingsDraft.maxConcurrentGlobal} onChange={(event) => onSettingsDraftChange({ ...settingsDraft, maxConcurrentGlobal: event.target.value })} />
              </Control>
              <Control label="Default poll interval">
                <input value={settingsDraft.defaultPollInterval} onChange={(event) => onSettingsDraftChange({ ...settingsDraft, defaultPollInterval: event.target.value })} />
              </Control>
              <Control label="Stall timeout">
                <input value={settingsDraft.stallTimeout} onChange={(event) => onSettingsDraftChange({ ...settingsDraft, stallTimeout: event.target.value })} />
              </Control>
              <Control label="Log retention">
                <input value={settingsDraft.logMaxFiles} onChange={(event) => onSettingsDraftChange({ ...settingsDraft, logMaxFiles: event.target.value })} />
              </Control>
            </div>
          </section>

          <section className="panel">
            <PanelHeader title="Current values" copy="Live values from the running config." meta="Runtime" />
            <div className="detailList compact">
              <Metric label="Global concurrency" value={String(config.max_concurrent_global)} />
              <Metric label="Default poll" value={config.default_poll_interval || "n/a"} />
              <Metric label="Stall timeout" value={config.stall_timeout || "n/a"} />
              <Metric label="Log retention" value={`${config.log_max_files} files`} />
            </div>
          </section>

          <section className="panel spanTwo">
            <PanelHeader title="Paths" copy="Config and filesystem locations currently backing this Maestro process." meta={config.config_path || "n/a"} />
            <div className="detailList sourceFilterGrid">
              <Metric label="Config path" value={config.config_path || "n/a"} />
              <Metric label="Workspace root" value={config.workspace_root || "n/a"} />
              <Metric label="State dir" value={config.state_dir || "n/a"} />
              <Metric label="Log dir" value={config.log_dir || "n/a"} />
            </div>
          </section>
        </div>
      ) : null}

      {settingsTab === "yaml" ? (
        <div className="settingsGrid">
          <section className="panel spanTwo">
            <PanelHeader title="Raw YAML editor" copy="Edit the live config carefully, validate it, review the diff, then save." meta={rawConfig?.editable ? "Editable" : "Read only"} />
            <textarea className="editor" value={configEditor} onChange={(event) => onConfigEditorChange(event.target.value)} />
            <div className="buttonRow">
              <button className="actionButton" onClick={onValidate}>Validate</button>
              <button className="actionButton" onClick={onDryRun}>Dry run</button>
              <button className="actionButton primary" onClick={onSave} disabled={!rawConfig?.editable}>Save</button>
            </div>
          </section>
          <section className="panel">
            <PanelHeader title="Validation and diff" copy="Use the real config loader before you save anything." meta="Safe path" />
            <p className="message">{configResult || "No validation run yet."}</p>
            <pre className="logBlock">{configDiff || "Run validate or dry run to see the diff."}</pre>
          </section>
        </div>
      ) : null}

      {settingsTab === "backups" ? (
        <div className="settingsGrid">
          <section className="panel">
            <PanelHeader
              title="Backups"
              copy="Config snapshots written before every save."
              meta={`${backups.length} backups`}
              actions={<button className="tinyButton primaryButton" onClick={onCreateBackup}>Create backup</button>}
            />
            <div className="stack">
              {backups.map((backup) => (
                <button
                  key={backup.name}
                  className={`listCard ${selectedBackupName === backup.name ? "selected" : ""}`}
                  onClick={() => onSelectBackup(backup)}
                >
                  <strong>{backup.name}</strong>
                  <span>{backup.created_at}</span>
                </button>
              ))}
            </div>
          </section>
          <section className="panel spanTwo">
            <PanelHeader
              title="Backup diff"
              copy="Compare a previous snapshot to the current config."
              meta={selectedBackup?.backup?.name || "No selection"}
              actions={selectedBackup ? <button className="tinyButton primaryButton" onClick={onRestoreBackup}>Restore backup</button> : null}
            />
            <pre className="logBlock">{selectedBackup?.diff || "Select a backup to inspect its diff."}</pre>
          </section>
        </div>
      ) : null}
    </section>
  );
}
