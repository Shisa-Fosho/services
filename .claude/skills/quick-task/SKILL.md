---
name: quick-task
description: Dispatch a task to the quick-task agent in the background, in an isolated git worktree. Usage: /quick-task "Add nginx reverse proxy"
user_invocable: true
arguments:
  - name: task
    description: Description of the task to execute
    required: true
---

# Quick Task Launcher

Dispatch the task to the quick-task agent using the `Agent` tool with ALL of the following parameters set:

- `subagent_type: "quick-task"`
- `run_in_background: true`
- `isolation: "worktree"` — CRITICAL: prevents the agent from mutating the user's local branch, index, or working directory
- `description`: a 3-5 word summary of `$ARGUMENTS`
- `prompt`: pass `$ARGUMENTS` verbatim — the agent's behavioral instructions (workflow, verification, reporting) live in `.claude/agents/quick-task.md`, not here

## After dispatching

Tell the user: "Dispatched to the quick-task agent in the background, in an isolated worktree. You'll get a notification when it completes. Continue working — your local branch is untouched."

Do nothing else. Do not implement the task yourself. Do not poll the agent — wait for the completion notification.
