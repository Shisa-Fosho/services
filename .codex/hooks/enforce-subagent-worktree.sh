#!/bin/bash
# PreToolUse hook: blocks `quick-task` subagent tool calls that didn't land in
# an isolated worktree. Catches the silent-isolation-failure mode observed on
# 2026-04-20 where a quick-task agent dispatched with isolation="worktree"
# still wrote into the main checkout.
#
# Scoped narrowly to agent_type="quick-task" so other subagents
# (general-purpose, Explore, Plan, skill-spawned helpers like /simplify or
# /commit-push-pr) keep their freedom to write into the main worktree.
#
# Decision logic:
#   1. agent_id absent on stdin → main agent → allow.
#   2. agent_type != "quick-task" → other subagent → allow.
#   3. quick-task subagent in a path containing "worktrees" → allow.
#   4. quick-task subagent anywhere else → deny.

set -u
INPUT=$(cat)

# Main agent: agent_id absent.
if ! printf '%s' "$INPUT" | grep -q '"agent_id"[[:space:]]*:[[:space:]]*"'; then
  exit 0
fi

AGENT_TYPE=$(printf '%s' "$INPUT" | sed -n 's/.*"agent_type"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

# Only enforce isolation for quick-task subagents.
if [ "$AGENT_TYPE" != "quick-task" ]; then
  exit 0
fi

CWD=$(printf '%s' "$INPUT" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

case "$CWD" in
  *worktrees*) exit 0 ;;
esac

AGENT_ID=$(printf '%s' "$INPUT" | sed -n 's/.*"agent_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
TOOL=$(printf '%s' "$INPUT" | sed -n 's/.*"tool_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

cat <<EOF
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"quick-task subagent ${AGENT_ID} attempted ${TOOL} from cwd=\"${CWD}\" — not an isolated worktree. quick-task must always run with isolation=\"worktree\" to protect the main checkout."}}
EOF
exit 0
