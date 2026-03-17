import { useEffect, useRef, useState } from "react";
import type { ConfigSourceSummary, ViewMode } from "../types";

export function Sidebar({
  view,
  sources,
  selectedWorkflow,
  search,
  onSearchChange,
  onSelectView,
  onOpenSource,
  onOpenSettings,
  onOpenPacks,
}: {
  view: ViewMode;
  sources: ConfigSourceSummary[];
  selectedWorkflow?: ConfigSourceSummary;
  search: string;
  onSearchChange: (value: string) => void;
  onSelectView: (view: ViewMode) => void;
  onOpenSource: (name: string) => void;
  onOpenSettings: () => void;
  onOpenPacks: () => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const selectorLabel = view === "overview" ? "Home" : selectedWorkflow?.name || "Select workflow";

  useEffect(() => {
    function handlePointer(event: MouseEvent) {
      if (!menuRef.current?.contains(event.target as Node)) {
        setMenuOpen(false);
      }
    }
    document.addEventListener("mousedown", handlePointer);
    return () => document.removeEventListener("mousedown", handlePointer);
  }, []);

  return (
    <aside className="sidebar">
      <div className="brandCard">
        <button className="brandMark brandHomeButton" onClick={() => onSelectView("overview")} aria-label="Open home">
          <img src="/favicon.svg" alt="" />
        </button>
        <div className="brandBlock">
          <button className="brandWordmark" onClick={() => onSelectView("overview")}>MAESTRO</button>
          <div className="brandMenu" ref={menuRef}>
            <button
              className="brandSelectorButton"
              type="button"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen((current) => !current)}
            >
              <span>{selectorLabel}</span>
              <span className="brandChevron">{menuOpen ? "⌃" : "⌄"}</span>
            </button>
            {menuOpen ? (
              <div className="brandMenuList" role="menu">
                <button
                  className="brandMenuItem"
                  type="button"
                  onClick={() => {
                    setMenuOpen(false);
                    onSelectView("overview");
                  }}
                >
                  <strong>Home</strong>
                  <span>Overview dashboard</span>
                </button>
                {sources.map((source) => (
                  <button
                    key={source.name}
                    className="brandMenuItem"
                    type="button"
                    onClick={() => {
                      setMenuOpen(false);
                      onOpenSource(source.name);
                    }}
                  >
                    <strong>{source.name}</strong>
                    <span>{source.tracker} workflow</span>
                  </button>
                ))}
              </div>
            ) : null}
          </div>
        </div>
      </div>

      <div className="sidebarMain">
        <div className="sidebarSearch">
          <input
            id="sidebar-search"
            type="search"
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder="Search"
          />
        </div>

        <section className="sidebarSection">
          <div className="sectionHeader">
            <span className="eyebrow">Workflows</span>
          </div>
          <div className="sidebarList">
            {sources.map((source) => (
              <button
                key={source.name}
                className={view === "workflow" && selectedWorkflow?.name === source.name ? "sidebarListItem active" : "sidebarListItem"}
                onClick={() => onOpenSource(source.name)}
              >
                <strong>{source.name}</strong>
                <span>{source.tracker} · {source.agent_type || "unmapped"}</span>
              </button>
            ))}
          </div>
        </section>
      </div>

      <div className="sidebarFooter">
        <button className={view === "settings" ? "navButton active" : "navButton"} onClick={onOpenSettings}>
          <span className="navIcon">⚙</span>
          <div>
            <strong>Settings</strong>
            <span>Runtime, YAML, backups</span>
          </div>
        </button>
        <button className={view === "packs" ? "navButton active" : "navButton"} onClick={onOpenPacks}>
          <span className="navIcon">✦</span>
          <div>
            <strong>Agent Packs</strong>
            <span>Prompts, tools, skills</span>
          </div>
        </button>
      </div>
    </aside>
  );
}
