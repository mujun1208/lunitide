---
name: brainstorming
description: Use before any creative work — new features, components, product direction, or behavior changes. Explores intent, constraints, and design before implementation.
---

# Brainstorming Ideas Into Designs

Turn vague ideas into validated designs through collaborative dialogue. **Do not write production code until the design is agreed.**

## Understanding the Idea

- Read project context first (workspace files, recent changes, existing docs)
- Ask **one question at a time**
- Prefer multiple-choice when possible
- Focus on: purpose, users, constraints, success criteria, non-goals

## Exploring Approaches

- Propose 2–3 approaches with trade-offs
- Lead with your recommendation and why
- Apply YAGNI — cut scope aggressively

## Presenting the Design

When ready, present the design in sections (~200–300 words each):

1. Problem & users
2. Core flows / architecture
3. UI or API surface (if applicable)
4. Data & error handling
5. Testing & rollout

After each section, ask whether it looks right before continuing.

## After the Design

- Write the validated design to `docs/plans/YYYY-MM-DD-<topic>-design.md` in the workspace
- Summarize open questions and next steps
- If implementation follows: hand off to `super-coders` or `pm-skill` as appropriate

## Principles

- One question per turn
- Incremental validation, not a wall of text
- Facts via tools — do not ask the user to look up things you can read
- No implementation until explicit go-ahead
