---
name: commit-push-pr
description: Stage, commit, push, and open a PR in one command
user_invocable: true
arguments:
  - name: message
    description: Optional commit message override
    required: false
---

# Commit, Push & PR

1. Stage relevant files (never stage secrets, binaries, or generated code)
2. Generate commit message from diff if not provided
3. Commit (no AI attribution in commit messages)
4. Push with `-u origin {branch}` if no upstream set
5. **Before creating the PR, re-read CLAUDE.md** to check for any rules about commit messages, PR bodies, or attribution. CLAUDE.md always overrides defaults in this skill.
6. Create PR if none exists, update if one does:
   - Title from branch/commit
   - Body with ## Summary, ## Test Plan
   - Add appropriate labels
   - **No AI attribution anywhere** — no "Generated with Claude", no "Co-Authored-By: Claude", nothing
7. Return the PR URL

**Never commit:**
- `.env` files or secrets
- `proto/gen/` (generated code)
- Binary files
- Node modules or vendor directories
