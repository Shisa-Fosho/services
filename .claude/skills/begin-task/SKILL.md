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
5. Create the branch via the **`create-branch`** skill, which guarantees the branch is based on a freshly fetched `origin/main` rather than the current HEAD. Never run ad-hoc `git checkout -b` here.
6. Move the issue to "In Progress" on the project board and assign it:
   - `gh issue edit {issue_number} --repo Shisa-Fosho/services --add-assignee @me`
   - Move to In Progress: `gh project item-edit --project-id PVT_kwDOEC3v5M4BUX8i --id {item_id} --field-id PVTSSF_lADOEC3v5M4BUX8izhBgiFs --single-select-option-id 47fc9ee4`
   - To get the item ID, run: `gh project item-list 2 --owner Shisa-Fosho --format json --limit 50 | node -e "const d=JSON.parse(require('fs').readFileSync(0,'utf8')); const i=d.items.find(x=>x.title.includes('{issue_title_fragment}')); if(i) console.log(i.id); else console.error('item not found');"`
7. Enter plan mode to discuss approach before writing code
8. Check if the issue has a parent and if so read the parent and any sibling issues so that you can gain full context around how the issue fits into the greater epic.
9. **Size check (during planning):** After exploring the codebase and designing the implementation, estimate the total hand-written LOC (excluding generated code and tests). The target is **≤ 800 LOC of production code per PR**.
   - If the estimate exceeds 800 LOC, do NOT proceed with a single large implementation. Instead:
     ```
     ⚠ SIZE LIMIT — estimated ~{N} LOC exceeds the 800 LOC target.
     Proposed split into sub-issues:
       1. "{sub-issue title}" (~{LOC} LOC) — {one-line scope}
       2. "{sub-issue title}" (~{LOC} LOC) — {one-line scope}
       ...
     ```
   - Present the split plan to the developer for approval. Each sub-issue should be independently mergeable and testable.
   - On approval, create the sub-issues via `gh issue create` with the parent issue referenced, then proceed with the first sub-issue only.
   - If the developer prefers to keep it as one PR, proceed but note the exception.

Branch naming examples:
- Issue #12 "CLOB matching engine" → `12-clob-matching-engine`
- Issue #5 "PostgreSQL schema" → `5-postgresql-schema`
