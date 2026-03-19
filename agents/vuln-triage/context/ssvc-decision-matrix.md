SSVC Deployer Decision Matrix:

Decision points:

EXPLOITATION:
  Active — CVE is in CISA KEV, OR EPSS > 0.7, OR scanner reports mature/attacked exploit
  PoC    — EPSS between 0.1 and 0.7, OR public proof-of-concept exists
  None   — EPSS < 0.1 AND not in KEV AND no known exploit

AUTOMATABLE (can attacker automate end-to-end?):
  Yes — CVSS AV:Network AND AC:Low AND PR:None AND UI:None
  No  — Any condition not met

TECHNICAL IMPACT:
  Total   — CVSS Confidentiality, Integrity, or Availability impact is High
  Partial — All impact metrics are Low or None

MISSION PREVALENCE (from repo/project labels):
  Essential — Labeled mission:essential or tier:1, or production-facing service
  Support   — Labeled mission:support or tier:2, or no label (default)
  Minimal   — Labeled mission:minimal or tier:3, experimental/archived

Decision mapping:

| Exploitation | Automatable | Tech Impact | Mission   | Decision |
|-------------|-------------|-------------|-----------|----------|
| Active      | Yes         | Total       | any       | ACT      |
| Active      | Yes         | Partial     | Essential | ACT      |
| Active      | Yes         | Partial     | Support   | ATTEND   |
| Active      | Yes         | Partial     | Minimal   | ATTEND   |
| Active      | No          | Total       | Essential | ACT      |
| Active      | No          | Total       | Support   | ATTEND   |
| Active      | No          | Total       | Minimal   | ATTEND   |
| Active      | No          | Partial     | any       | ATTEND   |
| PoC         | Yes         | Total       | Essential | ACT      |
| PoC         | Yes         | Total       | Support   | ATTEND   |
| PoC         | Yes         | Total       | Minimal   | TRACK*   |
| PoC         | Yes         | Partial     | any       | TRACK*   |
| PoC         | No          | Total       | Essential | ATTEND   |
| PoC         | No          | Total       | Support   | TRACK*   |
| PoC         | No          | any         | Minimal   | TRACK    |
| PoC         | No          | Partial     | any       | TRACK    |
| None        | Yes         | Total       | Essential | ATTEND   |
| None        | Yes         | Total       | Support   | TRACK*   |
| None        | Yes         | Partial     | any       | TRACK    |
| None        | No          | any         | any       | TRACK    |

Actions per decision:

ACT — Create remediation sub-issue (priority::critical, security::act). Include exact upgrade path. Escalate.
ATTEND — Create remediation sub-issue (priority::high, security::attend). Recommend next sprint.
TRACK* — Post triage comment (security::track-star). Re-triage in 30 days. No sub-issue.
TRACK — Post triage comment (security::track). Close as informational if EPSS < 0.05 and no fix and transitive-only.
NOT_APPLICABLE — Close with justification (security::not-applicable).
