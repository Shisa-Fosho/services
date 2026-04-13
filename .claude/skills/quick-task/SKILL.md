---
name: quick-task
description: Dispatch a task to an independent agent in a new terminal. Usage: /quick-task "Add nginx reverse proxy"
user_invocable: true
arguments:
  - name: task
    description: Description of the task to execute
    required: true
---

# Quick Task Launcher

Launch the quick-task agent in a new terminal window. Run this command:

```bash
start "" bash -c "claude --dangerously-skip-permissions --agent quick-task \"$ARGUMENTS\""
```

Tell the user: "Dispatched to a new terminal. The agent will create an issue, ask for your approval, implement, and open a PR. Switch to that terminal to interact with it."

Do nothing else. Do not implement the task yourself.
