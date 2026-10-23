# E3 checkout preview: attributed approval

Date: 23 October 2026. Package: ZF-244. Follows the web preview flow merged in
[#1171](https://github.com/eyupio/zoomies/pull/1171).

## Outcome

An operator can approve one exact retained checkout preview. Approval is a
separate act from keeping the preview: it names the preview, the commit and the
source digest that were reviewed, records who approved them, is audited, and
expires with the preview. It publishes nothing.

## Decisions

* **Approval is a row, not a rewrite.** Migration 0119 adds
  `optimisation_preview_approvals`, keyed by the preview and cascading from it.
  The immutable preview envelope and its storage digest are untouched, so replay
  still recomputes exactly what the reviewer kept. A read returns the approval
  beside the preview, never inside the evidence.
* **The binding is checked, not trusted.** The request names the head and source
  digest. The controller replays the retained preview from stored input, refuses
  anything that does not match, then asks the store to record the approval
  against the preview's storage digest. A moved head, a changed source or a
  corrupt record is a refusal.
* **The server names the actor.** The approver comes from the authenticated
  identity, as the reviewer did. The answer carries both `reviewed_by` and
  `approved_by`.
* **One approval per preview.** The first binding wins. Repeating the same
  approval returns the original record and writes no second audit row. A
  different binding is a conflict. The recommended default is kept: the same
  operator who reviewed may approve, with both people recorded. The owner's
  decision on requiring a different approver remains open and would be a later
  setting, not a silent change to this record.
* **Approval is not publication.** It reads no GitHub source, calls no model and
  consults neither optimisation setting: it neither reads source nor edits a
  workflow. It does not re-read the head, so `currentness` stays `not_checked`.
  Publication remains a later slice with its own opt-in and fresh head, policy
  and source checks.
* **Expiry follows the preview.** The store reads an approval only while its
  preview is inside the fourteen-day lifetime. Pruning or eviction of the
  preview cascades to the approval, and the audit row remains.
* **The page is the surface.** The retained preview card shows the commit and
  source digest, offers an approval declaration to an operator, and shows the
  recorded approver and time once approved. A viewer is not offered the form.

## Not in this slice

Opening a draft pull request, E2 verification of a published change, CLI, MCP
and Eli access. Eli still has no mutation tools for previews.

## Verification

Store: binding, scope, idempotency, audit rollback, corruption, expiry and
cascade. Controller: reviewer and approver recorded, moved binding refused,
replay carries the approval, expiry and cross-scope misses. API: route
authorisation and scopes, malformed and moved bindings, one audit row, replay
shape. Web: the declaration gates the button, the request names the preview's
own binding, the approved state replaces the form, and a phone does not overflow.
No live GitHub, model or fleet.
