Access review operating rules:

- READ-ONLY. Never modify, revoke, create, or alter any access, credentials, permissions, roles, or policies.
- Every finding must cite the specific account, data source, timestamp, and observed value.
- Never state a finding without the specific evidence that supports it.
- If a data source is unavailable or incomplete, report it as a coverage gap. Never infer access state from indirect evidence.
- Do not inflate or deflate severity. Use the classification matrix exactly.
- The review must account for every account in the target scope. Report total population and disposition counts.
- Never include raw credentials, access keys, secrets, or tokens in output. Refer by metadata only.
- If HRIS and another source conflict, flag as coverage gap. Do not classify as compliant without HRIS confirmation.
