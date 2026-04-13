---
name: create-issue
description: Create a new backlog issue with full planning doc alignment — assesses planning docs, proposes issue contents, places in build order, updates GitHub project + Notion docs
user_invocable: true
arguments:
  - name: description
    description: Brief description of what the issue should cover (can be rough — the skill will refine it)
    required: true
---

# Create Issue

Structured workflow for adding new items to the backlog. Ensures alignment across GitHub issues, the GitHub project board, and Notion planning docs.

**Invoke when:** We decide to create a new issue during a conversation, or the user says "create an issue for X."

## Step 1: Assess Current State

Gather context from three sources **in parallel**:

### 1a. Fetch the current GitHub backlog
```bash
gh issue list --repo Shisa-Fosho/services --state all --limit 100 --json number,title,state,labels
```

### 1b. Fetch the Notion Build Order doc
```
notion-fetch: 33092032f61981b6b762de2bab90eaf8
```
Extract the Issue Key table and Blocking Dependencies table.

### 1c. Fetch the Notion Technical & Product Plan
```
notion-fetch: 32b92032f619811aa868f4d75f5017c8
```
Scan for sections relevant to the proposed issue — look for requirements, scope, or design decisions that should inform the issue description.

### 1d. Review the active conversation
Read back through the current chat to capture any decisions, context, or constraints discussed that should be reflected in the issue.

## Step 2: Duplicate & Overlap Check

Before proposing the issue:

1. **Search existing issues** for keyword overlap with the proposed work:
   ```bash
   gh issue list --repo Shisa-Fosho/services --state all --search "<keywords>" --json number,title,state
   ```
2. **Check the Notion Technical Plan** for whether this work is already scoped under an existing item.
3. If overlap is found, **tell the user** and ask whether to:
   - Extend the existing issue instead
   - Create a new issue that references the existing one
   - Proceed with a new standalone issue

## Step 3: Propose Issue Contents

Present a draft to the user with:

```
## Proposed Issue

**Title:** <concise title, prefixed with build-order ID if applicable>
**Labels:** <inferred from component — use existing label set>

**Body:**
## Parent Context
<link to parent issue or Notion section>

## Description
<what and why>

## Scope
<bullet list of deliverables>

## Estimated LoC
<rough estimate if applicable>

## Acceptance Criteria
- [ ] <testable criteria>

## Dependencies
- **blocked-by:** <issue refs>
- **blocks:** <issue refs>
```

**Label inference rules:**
- `internal/platform/` → `component:shared-platform`
- `internal/trading/` or `cmd/trading/` → `component:trading-service`
- `internal/market/` or `internal/data/` or `cmd/platform/` → `component:platform-service`
- New feature → `type:feature`
- Bug fix → `type:bug`
- Cleanup/refactor → `type:chore`

**Ask the user:**
- Confirm the issue contents
- Any adjustments to scope, dependencies, or labels
- Clarify anything ambiguous from the conversation

Do NOT proceed until the user confirms.

## Step 4: Propose Build Order Placement

Fetch the current build order from the GitHub project board:
```
gh api graphql -f query='{ organization(login: "Shisa-Fosho") { projectV2(number: 2) { items(first: 50) { nodes { bo: fieldValueByName(name: "Build Order") { ... on ProjectV2ItemFieldNumberValue { number } } phase: fieldValueByName(name: "Phase") { ... on ProjectV2ItemFieldSingleSelectValue { name } } content { ... on Issue { title number } ... on DraftIssue { title } } } } } } }'
```

Present the current order and propose where the new issue slots in:

```
Current build order around the insertion point:
  BO=N   | Phase X | <item before>
  BO=N+1 | Phase X | ← NEW: <proposed issue title>
  BO=N+2 | Phase X | <item after>

Phase: <proposed phase>
Rationale: <why it goes here — what it depends on and what depends on it>
```

**Rules for placement:**
- If it's a subtask of an existing parent, use parent.N numbering (e.g., 8.5)
- If inserting between existing items, renumber downstream items to maintain integer ordering
- Check that blocked-by items have a lower build order number
- Check that blocks items have a higher build order number

**Dependency validation (run before presenting to user):**
1. For each `blocked-by` reference: verify the referenced issue exists, is in a lower or equal build order position, and is in the same or earlier phase. Flag violations.
2. For each `blocks` reference: verify the referenced issue exists and is in a higher build order position. Flag violations.
3. Check for **circular dependencies** — if A blocks B and B blocks A (directly or transitively), flag it.
4. If any violations are found, present them alongside the proposal with a suggested fix (e.g., "this should be in Phase 2 instead of Phase 3 because it's blocked by a Phase 2 item").

**Build order gap detection:**
1. After determining the insertion point, scan for numbering gaps or collisions in the current build order.
2. If the insertion creates a collision (e.g., two items at BO=16), renumber downstream items before presenting.
3. If there's already a gap at the right position (e.g., nothing between BO=15 and BO=17), use the gap rather than renumbering.

**Ask the user** to confirm placement. Do NOT proceed until confirmed.

## Step 5: Create & Update Everything

Once the user confirms both the issue contents and placement, execute all updates:

### 5a. Create the GitHub issue
```bash
gh issue create --repo Shisa-Fosho/services --title "<title>" --label "<labels>" --body "<body>"
```

### 5b. Add to GitHub project board and set fields
```bash
# Add to project
gh project item-add 2 --owner Shisa-Fosho --url <issue_url>

# Get the item ID
gh project item-list 2 --owner Shisa-Fosho --format json --limit 50

# Set Phase
gh project item-edit --project-id PVT_kwDOEC3v5M4BUX8i --id <item_id> --field-id PVTSSF_lADOEC3v5M4BUX8izhBgiKE --single-select-option-id <phase_option_id>

# Set Build Order
gh project item-edit --project-id PVT_kwDOEC3v5M4BUX8i --id <item_id> --field-id PVTF_lADOEC3v5M4BUX8izhBgiK8 --number <build_order>
```

**Phase option IDs:**
- `ff13e535` — Phase 0: Foundation
- `a9afd19d` — Phase 1: Domain Types
- `5e1a5684` — Phase 2: First Flow
- `52a6971d` — Phase 3: Frontend + APIs
- `bd9808b9` — Phase 4: On-Chain Workers
- `9be4cd6d` — Phase 5: Bot
- `ebeb2cff` — Phase 6: Pre-Launch

**Project field IDs:**
- Project: `PVT_kwDOEC3v5M4BUX8i`
- Status: `PVTSSF_lADOEC3v5M4BUX8izhBgiFs` (options: `f75ad846`=Todo, `47fc9ee4`=In Progress, `98236657`=Done)
- Phase: `PVTSSF_lADOEC3v5M4BUX8izhBgiKE`
- Build Order: `PVTF_lADOEC3v5M4BUX8izhBgiK8`

### 5c. Renumber downstream items if needed
If the insertion required renumbering (not using .N subtask notation), update all affected items' Build Order numbers.

### 5d. Update Notion Build Order doc
```
notion-update-page: 33092032f61981b6b762de2bab90eaf8
```
- Add the new item to the Issue Key table in the correct position
- Add it to the Blocking Dependencies table
- Update any existing items whose "blocks" column should now include the new issue

### 5e. Update Notion Technical & Product Plan (if applicable)
```
notion-update-page: 32b92032f619811aa868f4d75f5017c8
```
- Add issue number references in the relevant section for traceability
- Only update if the new issue maps to a specific section in the tech plan

## Step 6: Report

Provide a concise summary of everything that changed:

```
## Issue Created
- **#<number>:** <title>
- **Phase:** <phase>
- **Build Order:** <number>
- **Labels:** <labels>
- **Blocked by:** <refs>
- **Blocks:** <refs>

## Project Board Changes
- Added #<number> at BO=<N>, Phase <X>
- <any renumbered items>

## Notion Updates
- Build Order doc: added to Issue Key table and Blocking Dependencies table
- Technical Plan: <what was updated, or "no changes needed">

## Consistency Check
- ✓ All blocked-by items have lower build order
- ✓ All blocks items have higher build order
- ✓ No duplicate/overlapping issues
- ✓ Notion and GitHub are aligned
```
