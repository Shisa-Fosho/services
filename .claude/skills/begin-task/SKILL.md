---
name: begin-task
description: Start working on a GitHub issue — reads the issue, creates a branch, and enters planning mode
user_invocable: true
arguments:
  - name: issue_number
    description: The GitHub issue number to work on
    required: true
---

# Begin Task

1. Read the GitHub issue: `gh issue view {issue_number} --repo Shisa-Fosho/services`
2. **Check for blocking dependencies** before proceeding:
   - Parse the issue body for "blocked-by" or "blocked by" references to other issues (e.g., `#4`, `#7`)
   - For each blocker, run `gh issue view {blocker_number} --repo Shisa-Fosho/services --json state,title,number` to check if it's still open
   - If ANY blockers are still **open**, warn the developer clearly:
     ```
     ⚠ BLOCKERS DETECTED — this issue has open dependencies:
       • #{number} "{title}" (state: OPEN)
     ```
   - Ask the developer whether to proceed anyway (they may have a stub/workaround strategy) or switch to working on a blocker first. Do NOT silently continue.
   - If all blockers are closed, note this and proceed normally.
3. Parse the issue title to derive a branch name: `{issue_number}-{short-kebab-description}`
4. Ensure working tree is clean: `git status --porcelain`
5. Create and checkout branch from main: `git checkout main && git pull && git checkout -b {branch_name}`
6. Enter plan mode to discuss approach before writing code

Branch naming examples:
- Issue #12 "CLOB matching engine" → `12-clob-matching-engine`
- Issue #5 "PostgreSQL schema" → `5-postgresql-schema`
