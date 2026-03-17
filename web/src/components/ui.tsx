import type { ReactNode } from "react";

export function PanelHeader({
  title,
  copy,
  meta,
  actions,
}: {
  title: string;
  copy: string;
  meta?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="panelHeader">
      <div>
        <h3>{title}</h3>
        <p>{copy}</p>
      </div>
      <div className="panelHeaderMeta">
        {meta ? <span>{meta}</span> : null}
        {actions}
      </div>
    </div>
  );
}

export function Control({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="control">
      <span>{label}</span>
      {children}
    </label>
  );
}

export function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

export function Pill({ children, tone }: { children: ReactNode; tone?: "ok" | "warn" | "info" }) {
  return <span className={`pill ${tone || ""}`}>{children}</span>;
}

export function Badge({ label, value, tone = "neutral" }: { label: string; value: string; tone?: "neutral" | "warn" }) {
  return (
    <span className={`headerBadge ${tone}`}>
      <strong>{value}</strong>
      <span>{label}</span>
    </span>
  );
}

export function EmptyState({ copy }: { copy: string }) {
  return <div className="emptyState">{copy}</div>;
}
