---
name: super-coders
description: Turn approved specs into executable dev work — split vertical-slice tasks, implement with tests, and review. Use after brainstorming or pm-skill when ready to build.
---

# Super Coders

Bridge from product spec to working code with disciplined execution.

## Preconditions

- Confirm spec or design doc exists (path in workspace)
- Confirm scope for this session (one tracer bullet or a small batch)
- Pick execution mode with the user (approval vs auto-edit)

## Step 1 — Split Work

- Break into **vertical slices** (end-to-end demonstrable increments)
- Each task: title, goal, files likely touched, acceptance criteria, dependencies
- Prefer `to-tickets` patterns; output markdown task list in workspace

## Step 2 — Implement One Slice

For the current task only:

1. Restate acceptance criteria
2. Write/adjust tests first when feasible (`tdd-loop` mindset)
3. Use `workspace.read` / `workspace.edit` / `command.run` (whitelist only)
4. Run verification commands before marking done

## Step 3 — Review

- Self-review against spec; cite path:line for issues
- Invoke `code-reviewer` skill or summarize diff for user review
- Do not start the next slice until the current one is accepted or user says continue

## Step 4 — Parallel Work (Optional)

- For independent tasks, use `subagent.spawn` + `subagent.join`
- Merge results; resolve conflicts explicitly

## Anti-patterns

- Horizontal splits ("only backend" with no demo)
- Large unreviewed diffs
- Skipping verification because "it should work"
