---
name: align-planning
description: Cross-check and align all planning sources — GitHub issues, GitHub project board, Notion Build Order doc, and Notion Technical Plan. Finds and fixes drift between them.
---

# Align Planning

Audits all planning sources for consistency and fixes any drift. Run this after any change to the backlog — new issues, reordering, scope changes, architectural decisions, or issue updates.

**Invoke when:**

- After creating or updating GitHub issues
- After changing the project board (build order, phase, status)
- After making architectural changes that affect service responsibilities
- After any conversation where we decide to move, rename, or rescope work
- As the final step of the `/create-issue` skill
- Whenever the user says "align", "sync planning", "check alignment", or similar

## Sources of Truth

| Source                    | What it contains                                                       | ID / Location                               |
| ------------------------- | ---------------------------------------------------------------------- | ------------------------------------------- |
| **GitHub Issues**         | Issue titles, descriptions, scope, dependencies, labels, state         | `gh issue list --repo Shisa-Fosho/services` |
| **GitHub Project Board**  | Build Order numbers, Phase assignments, Status                         | Project #2, org Shisa-Fosho                 |
| **Notion Build Order**    | Issue Key table, Blocking Dependencies table, dependency graphs        | `33092032f61981b6b762de2bab90eaf8`          |
| **Notion Technical Plan** | Architecture, service responsibilities, component ownership, MVP scope | `32b92032f619811aa868f4d75f5017c8`          |

## Step 1: Gather Current State (in parallel)

### 1a. GitHub Issues

```bash
gh issue list --repo Shisa-Fosho/services --state all --limit 100 --json number,title,state,labels,body
```

### 1b. GitHub Project Board

```
gh api graphql -f query='{ organization(login: "Shisa-Fosho") { projectV2(number: 2) { items(first: 50) { nodes { bo: fieldValueByName(name: "Build Order") { ... on ProjectV2ItemFieldNumberValue { number } } phase: fieldValueByName(name: "Phase") { ... on ProjectV2ItemFieldSingleSelectValue { name } } status: fieldValueByName(name: "Status") { ... on ProjectV2ItemFieldSingleSelectValue { name } } content { ... on Issue { title number } ... on DraftIssue { title } } } } } } }'
```

### 1c. Notion Build Order doc

```
notion-fetch: 33092032f61981b6b762de2bab90eaf8
```

### 1d. Notion Technical Plan

```
notion-fetch: 32b92032f619811aa868f4d75f5017c8
```

## Step 2: Cross-Check for Drift

Run each check and collect findings. Do NOT fix anything yet — gather all findings first.

### 2a. Title Consistency

For each item in the Notion Issue Key table, check that the corresponding GitHub issue title matches. Flag mismatches.

### 2b. Build Order Consistency

Compare the ordering in the Notion Issue Key table against the GitHub project board Build Order numbers. Flag items that appear in a different sequence.

### 2c. Phase Consistency

Compare the Phase column in Notion vs the Phase field on the GitHub project board. Flag mismatches.

### 2d. Dependency Consistency

For each item in the Notion Blocking Dependencies table:

1. Check that every "blocked-by" reference is bidirectional — if A is blocked by B, then B's "blocks" column should list A.
2. Check that blocked-by items have a lower or equal build order number on the project board.
3. Check that blocks items have a higher build order number.
4. Check that the GitHub issue body's Dependencies section matches the Notion dependencies table.
5. Flag any circular dependencies.

### 2e. Scope / Architecture Consistency

Check the Notion Technical Plan's service responsibility assignments against what the code actually does:

- Which service owns which endpoints (check nginx.conf routing + cmd/\*/main.go wiring)
- Which components are listed under which service in the Tech Plan
- Flag any component listed in the wrong service

### 2f. Coverage Check

1. Every item in the Notion Build Order should have a corresponding GitHub issue.
2. Every GitHub issue with a build-order ID (S1, T3a, P2.1, SEC1, etc.) should appear in the Notion Build Order.
3. MVP scope items in the Technical Plan should be tracked by at least one GitHub issue.
4. Flag any items on the project board that are missing from Notion, or vice versa.

### 2g. Status Consistency

- GitHub issues marked as CLOSED should have status "Done" on the project board.
- Items with status "In Progress" on the board should have an open GitHub issue.

## Step 3: Present Findings For Approval and Iterate With User if Needed

Present all findings in a table:

```
## Alignment Audit

| # | Category | Source A | Source B | Issue | Severity |
|---|----------|---------|---------|-------|----------|
| 1 | Title    | GitHub #31 "P2.1: Admin Wallet..." | Notion "P2.1: HTTP Utilities..." | Title mismatch | Medium |
| 2 | Phase    | GitHub Board: Phase 3 | Notion: Phase 2 | SEC1 phase drift | Medium |
| ... | | | | | |

**No issues found:** ✓ (if everything is aligned)
```

Severity levels:

- **High:** Contradictory information that could cause wrong work (e.g., component in wrong service)
- **Medium:** Stale data that could confuse (e.g., title mismatch, dependency mismatch)
- **Low:** Minor cosmetic drift (e.g., slightly different wording)

If there are no findings, confirm alignment is clean and skip to Step 5.

## Step 4: Once User approves Fix Drift

For each finding, fix it in the source that's stale. The priority order for "which source is correct" is:

1. **Code / nginx.conf** — what the system actually does is always right
2. **GitHub Issues** — the most recently updated description wins
3. **GitHub Project Board** — build order and phase numbers
4. **Notion docs** — updated to match the above

Apply fixes:

### Notion Build Order updates

```
notion-update-page: 33092032f61981b6b762de2bab90eaf8
```

### Notion Technical Plan updates

```
notion-update-page: 32b92032f619811aa868f4d75f5017c8
```

### GitHub Project Board updates

```bash
# Phase
gh project item-edit --project-id PVT_kwDOEC3v5M4BUX8i --id <item_id> --field-id PVTSSF_lADOEC3v5M4BUX8izhBgiKE --single-select-option-id <phase_option_id>

# Build Order
gh project item-edit --project-id PVT_kwDOEC3v5M4BUX8i --id <item_id> --field-id PVTF_lADOEC3v5M4BUX8izhBgiK8 --number <build_order>

# Status
gh project item-edit --project-id PVT_kwDOEC3v5M4BUX8i --id <item_id> --field-id PVTSSF_lADOEC3v5M4BUX8izhBgiFs --single-select-option-id <status_option_id>
```

### GitHub Issue updates (if needed)

```bash
gh issue edit <number> --repo Shisa-Fosho/services --title "<title>" --body "<body>"
```

**Phase option IDs:**

- `ff13e535` — Phase 0: Foundation
- `a9afd19d` — Phase 1: Domain Types
- `5e1a5684` — Phase 2: First Flow
- `52a6971d` — Phase 3: Frontend + APIs
- `bd9808b9` — Phase 4: On-Chain Workers
- `9be4cd6d` — Phase 5: Bot
- `ebeb2cff` — Phase 6: Pre-Launch

**Status option IDs:**

- `f75ad846` — Todo
- `47fc9ee4` — In Progress
- `98236657` — Done

**Project field IDs:**

- Project: `PVT_kwDOEC3v5M4BUX8i`
- Status: `PVTSSF_lADOEC3v5M4BUX8izhBgiFs`
- Phase: `PVTSSF_lADOEC3v5M4BUX8izhBgiKE`
- Build Order: `PVTF_lADOEC3v5M4BUX8izhBgiK8`

## Step 5: Report

```
## Alignment Report

**Sources checked:** GitHub Issues, Project Board, Notion Build Order, Notion Technical Plan
**Findings:** <N> issues found, <M> fixed

### Fixes Applied
- <what changed, where>

### Still Aligned
- ✓ Titles match across GitHub and Notion
- ✓ Build order consistent between project board and Notion
- ✓ Phases consistent
- ✓ Dependencies bidirectional and order-consistent
- ✓ Service ownership matches code + nginx routing
- ✓ All MVP scope items tracked
- ✓ Statuses consistent
```
