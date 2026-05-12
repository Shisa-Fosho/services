---
name: create-branch
description: Create a new git branch safely — always from a freshly fetched origin/main, never from the currently checked-out branch. Use whenever a new branch is needed (starting an issue, spinning off a skill/config tweak, hotfix, etc.).
---

# Create Branch

Always branch from **remote `origin/main`** — never from whatever branch is currently checked out.

## Why this exists

Branching from the current branch inherits its commits. If that branch holds pre-squash commits of an already-merged PR (common when the PR was squash-merged on GitHub), the new branch will include those commits as "new" work, polluting any PR opened from it. The fix is to always base off `origin/main` directly.

## Steps

1. **Check working tree is clean:**
   ```bash
   git status --porcelain
   ```
   If there are uncommitted changes, STOP and surface them to the user. Do not silently stash or discard — the user must decide (stash, commit to a dedicated branch, or abandon).

2. **Fetch latest from remote:**
   ```bash
   git fetch origin
   ```

3. **Create the branch directly from `origin/main`:**
   ```bash
   git checkout -b {branch_name} origin/main
   ```
   This form bypasses the local `main` branch entirely. Never use `git checkout main && git pull && git checkout -b ...` — that path fails silently if you're on an unrelated branch, if local `main` has drifted, or if a squash-merge left old commits reachable from your current HEAD.

4. **Verify the branch is where you expect:**
   ```bash
   git log --oneline origin/main..HEAD
   ```
   Expect **zero commits** output. If anything appears, the branch was not created from a clean base — stop and investigate before committing anything.

## Anti-patterns (do NOT do these)

- `git checkout -b <name>` (no explicit base) — branches from current HEAD, the exact bug this skill prevents.
- `git checkout main && git pull && git checkout -b <name>` — assumes local `main` tracks `origin/main` correctly and that you're able to switch to it cleanly. Fails when the working tree is dirty or local `main` has diverged.
- Branching without fetching first — you may base off stale refs.
