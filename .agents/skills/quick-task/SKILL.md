---
name: quick-task
description: Dispatch a task to a Codex worker subagent in the background using an isolated worktree-style workflow. Use only when the user explicitly asks for quick-task, delegation, a background agent, or parallel agent work.
---

# Quick Task Launcher

Dispatch the task to a Codex `worker` subagent only when the user explicitly asks for delegation or background agent work.

Use `spawn_agent` with `agent_type: "worker"` and a prompt based on `.codex/agents/quick-task.md`. Tell the worker it is not alone in the codebase, must not revert edits made by others, and must list changed paths in its final response.

## After dispatching

Tell the user: "Dispatched to a worker agent in the background. Continue working; I will integrate or report its result when needed."

Do nothing else. Do not implement the task yourself. Do not poll the agent — wait for the completion notification.
