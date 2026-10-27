# Verifying a PR against a ClickUp task's acceptance criteria

This procedure is for the QA agent. Bitbucket data comes from
`prismatic-rainbow`. ClickUp data comes from the agent's ClickUp tool (for
example the official ClickUp MCP), because this CLI does not call ClickUp.

## 1. Link PR and task

From a PR:

```bash
prismatic-rainbow pr links 42 -o json
```

Use the `references[]` entries with `confidence: "high"`. Treat a `low`
entry (`#id`) as a task only after the ClickUp lookup succeeds. ClickUp IDs are
case-insensitive and are returned lowercased. Custom IDs (`ENG-123`) need the
prefix configured (`--custom-prefix ENG` or `CLICKUP_CUSTOM_ID_PREFIXES`). Look
them up in ClickUp with `custom_task_ids=true` and the team id.

From a task:

```bash
prismatic-rainbow pr list --state all --task CU-86b1x2y3z -o json
prismatic-rainbow pr list --state all --task 86b1x2y3z -o json   # bare id in branch/title/description
prismatic-rainbow branch list --match 86b1x2y3z -o json          # branch exists but no PR yet
```

A task can have several PRs (one per repo, or follow-up fixes). Check every
PR.

## 2. Collect the PR state

```bash
prismatic-rainbow pr context 42 -o json
```

Record the following:

- `pull_request.state` (OPEN/MERGED/DECLINED/SUPERSEDED), `draft`,
  `destination.branch`.
- `pull_request.review`: approvals, changes_requested, pending reviewers.
- `checks.overall` and the names of failed checks.
- `mergeability.blocking` (ignore `current_user_permission_check` when you
  have a read-only token).
- `threads.unresolved` and `tasks.unresolved`.
- `files`: the changed paths. Tests are the paths matching `test`, `spec`,
  `__tests__` or `e2e`.
- Every `warnings[]` entry. A missing section means that evidence is unknown,
  not that it is fine.

## 3. Read the task

From ClickUp, get:

- The description as markdown.
- Checklists (`checklists[].items[]`).
- Custom fields (teams often keep "Acceptance Criteria" or "Criterios de
  aceptación" there).
- Status and subtasks.

Extract the acceptance criteria in this order:

1. A heading named Acceptance Criteria, Criterios de aceptación, AC or DoD,
   and its bullet or checkbox list.
2. Checklists whose name contains acceptance, criterios, AC or QA.
3. A matching custom field.
4. If none exists, derive testable statements from the description and mark
   the report `criteria_source: "derived"`.

## 4. Gather evidence per criterion

Map each criterion to the files that should implement it. Search the changed
paths and use `pr diff --name-only` first. Then read only what you need:

```bash
prismatic-rainbow pr diff 42 --path src/payments -o json
prismatic-rainbow pr file 42 src/payments/refund.ts --lines 40:120
prismatic-rainbow pr file 42 src/payments/refund.ts --side destination   # behavior before the PR
```

Verdicts:

- `met`: code implements it, and ideally a test asserts it. Cite `path:line`.
- `partially_met`: some cases, branches or platforms are missing. Say which.
- `not_met`: no change addresses it, or the change contradicts it.
- `not_verifiable_statically`: it depends on runtime, UI, data or config
  outside the repo. Say what manual check is needed.

Do not count the PR description or commit messages as evidence. They are
claims to verify against the code.

## 5. Report

```json
{
  "task": {"id": "86b1x2y3z", "status": "in review", "criteria_source": "description"},
  "pull_requests": [{"id": 42, "state": "OPEN", "checks": "pass", "mergeable": false,
                     "blocking": ["minimum_approvals"], "unresolved_threads": 1}],
  "criteria": [
    {"criterion": "Refund is rejected when amount > captured", "verdict": "met",
     "evidence": ["src/payments/refund.ts:57 guard", "src/payments/refund.test.ts:88 test"]},
    {"criterion": "User sees an error toast", "verdict": "not_verifiable_statically",
     "evidence": ["src/ui/RefundForm.tsx:31 calls showToast"], "manual_check": "trigger refund > captured in staging"}
  ],
  "status_consistency": [
    "Task is 'in review' and PR is OPEN with passing checks: consistent"
  ],
  "summary": "3/4 criteria met, 1 needs manual QA. Not ready to close: 1 unresolved review thread."
}
```

Only post this report to the PR (`pr comment 42 --body-file - < report.md`)
or change the ClickUp task when the user asked for it.
