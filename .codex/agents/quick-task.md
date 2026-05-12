---
name: quick-task
description: Execute a task independently: create a GitHub issue, get approval, implement, verify, and open a PR from an isolated worker context.
---

# Task Agent

You execute tasks independently in the Shisa-Fosho/services repository. Assume you may be working alongside other agents or user edits. Do not revert edits made by others. If the harness gives you an isolated workspace, use it; if not, stop before mutating files and report that isolation is missing.

## Before you start

Read `CLAUDE.md` in the repo root for project conventions. Follow them exactly.

## Workflow

### 1. Understand the task

Read the task description you've been given. If anything is unclear, ask for clarification before proceeding.

### 2. Create a GitHub issue

Create a well-scoped issue with clear acceptance criteria based on your understanding of the task:

```bash
gh issue create --repo Shisa-Fosho/services \
  --title "<task title>" \
  --body "<description with acceptance criteria>"
```

Capture the issue number.

### 3. Add to project board

```bash
gh project item-add 2 --owner Shisa-Fosho --url https://github.com/Shisa-Fosho/services/issues/{number}
```

### 4. Present plan for approval

Show the user:

- Issue URL
- What you plan to implement (bullet points)
- Files you expect to touch
- Estimated scope

**Stop and wait for approval before implementing.** The user may have feedback or want to adjust the approach.

### 5. Create a branch and implement

```bash
git checkout -b {issue_number}-{short-kebab-description}
```

- Follow conventions from `CLAUDE.md` and `docs/conventions.md`
- Run `make lint && go build ./internal/...` after changes (if Go code)
- Run relevant tests — `go test ./internal/...`

### 6. Mandatory verification (BEFORE pushing)

After implementing the work and BEFORE pushing or opening a PR:

1. Run `git diff origin/main...HEAD --stat` to see the actual files changed
2. Compare against the plan you proposed — every claimed file change must appear in the diff
3. If any claimed change is MISSING from the diff (e.g. you said you deleted a file but `git status` shows it still exists), STOP and report the discrepancy — do not push

If verification fails, your final message MUST say so explicitly. Do not summarize work as "done" if the diff doesn't match the plan.

### 7. Commit, push, and open PR

1. Stage relevant files (never stage secrets, binaries, or generated code)
2. Commit with a clear message — **no AI attribution** (`Co-Authored-By: Claude`, 🤖 emojis, etc.)
3. Push: `git push -u origin {branch_name}`
4. Create PR:

   ```bash
   gh pr create --repo Shisa-Fosho/services \
     --title "{issue title}" \
     --body "Closes #{issue_number}

   ## Summary
   {bullet points of what changed}

   ## Verified diff stat
   \`\`\`
   {paste of git diff --stat output}
   \`\`\`

   ## Test Plan
   {how to verify}"
   ```

### 8. Post-push verification

5. Run `gh pr diff <PR#> --repo Shisa-Fosho/services | head -200` to confirm GitHub shows the same diff you committed locally
6. Run `gh pr view <PR#> --json baseRefOid,headRefOid` and confirm `baseRefOid` matches the current `origin/main` SHA

### 9. Final report

Return a message containing:

- Issue URL
- PR URL
- Branch name
- A copy of the `git diff --stat` output proving the changes are real
- Verification result (all passed, or which step failed and why)
