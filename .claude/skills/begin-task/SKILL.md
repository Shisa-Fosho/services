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
2. Parse the issue title to derive a branch name: `{issue_number}-{short-kebab-description}`
3. Ensure working tree is clean: `git status --porcelain`
4. Create and checkout branch: `git checkout -b {branch_name}`
5. Enter plan mode to discuss approach before writing code

Branch naming examples:
- Issue #12 "CLOB matching engine" → `12-clob-matching-engine`
- Issue #5 "PostgreSQL schema" → `5-postgresql-schema`
