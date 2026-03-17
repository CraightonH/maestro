import type { Approval } from "../types";
import { latestApprovalText } from "../lib/helpers";

export function ApprovalBanner({
  approval,
  approvals,
  onResolve,
}: {
  approval?: Approval;
  approvals: number;
  onResolve: (requestId: string, action: "approve" | "reject") => Promise<void>;
}) {
  if (!approval) {
    return null;
  }

  return (
    <section className="approvalBanner">
      <div className="approvalCopy">
        <strong>{approvals} approval{approvals === 1 ? "" : "s"} waiting</strong>
        <span>{latestApprovalText(approval)}</span>
      </div>
      <div className="buttonRow">
        <button className="actionButton primary" onClick={() => void onResolve(approval.request_id, "approve")}>Approve</button>
        <button className="actionButton" onClick={() => void onResolve(approval.request_id, "reject")}>Reject</button>
      </div>
    </section>
  );
}
