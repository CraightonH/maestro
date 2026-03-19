Access review severity classification:

CRITICAL (SLA: 4 hours)
  Active exploitation risk OR terminated-employee access confirmed used post-termination
  OR credentials confirmed exposed publicly.
  Examples: terminated employee with active admin credentials used after termination,
  break-glass account used routinely without audit logging.

HIGH (SLA: 7 days)
  Direct compliance violation that would result in an audit finding.
  Privileged access without current business justification.
  Missing required controls.
  Examples: orphaned admin service account, user with prod admin and no justification,
  MFA not enforced on privileged accounts, access keys > 90 days on privileged accounts.

MEDIUM (SLA: 30 days)
  Policy deviation without immediate risk. Least-privilege violations where access is
  partially justified. Credential hygiene issues.
  Examples: role drift (changed teams, retains old permissions), direct IAM policy
  attachment instead of group-based, non-privileged access keys > 90 days.

LOW (SLA: 90 days / next review cycle)
  Cleanup items. No active risk but creates maintenance burden.
  Examples: dormant non-privileged accounts, unused SSO app assignments, empty IAM groups.
  Escalate to MEDIUM if same LOW finding appears in 3+ consecutive review cycles.

Sub-issue creation rules:
  CRITICAL / HIGH: Individual sub-issue per finding, assigned to system owner, due by SLA.
  MEDIUM: Grouped sub-issue per system owner collecting all their MEDIUM findings, due 30 days.
  LOW: Document in report only. No sub-issues unless 3+ consecutive cycles.
