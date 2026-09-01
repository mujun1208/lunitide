---
name: obra-superpowers-agentic-workflow
description: Agentic skills framework for coding agents providing structured workflows for TDD, planning, debugging, and subagent-driven development. Use when the user mentions Superpowers, brainstorming before code, design-first planning, or "use the brainstorming skill".
---

# obra/superpowers Agentic Skills Framework

> Installed locally for Cursor from [obra/superpowers](https://github.com/obra/superpowers) and [ara.so trending-skills](https://github.com/aradotso/trending-skills/blob/main/skills/obra-superpowers-agentic-workflow/SKILL.md).

Superpowers gives coding agents structured workflows: design before code, TDD, and plan-then-execute. In this repo the installed subset lives under `.cursor/skills/`.

## What is installed here

| Skill | Path | When to use |
|---|---|---|
| `using-superpowers` | `.cursor/skills/using-superpowers/SKILL.md` | Session start: invoke the matching skill before acting |
| `brainstorming` | `.cursor/skills/brainstorming/SKILL.md` | Any creative / feature / behavior change — design first |
| `writing-plans` | `.cursor/skills/writing-plans/SKILL.md` | After an approved architectural spec, before code |

The rest of the official suite (TDD, git worktrees, subagent-driven-development, systematic-debugging, …) is **not** copied into this repo. Install those later if a later step needs them.

## Core workflow (in order)

1. **Brainstorming** — classify spike / bounded / architectural. Ask one question at a time. Do not write code until the human approves the design.
2. **writing-plans** — only after an architectural spec is approved. Bite-sized TDD tasks.
3. **Execute** — TDD / subagents / inline batches. Not in this install.

## Cursor usage

In Agent chat, say any of:

- `用 Superpowers 的 Brainstorm`
- `Before writing any code, use the brainstorming skill`
- `help me plan this feature`

The agent must read `.cursor/skills/brainstorming/SKILL.md` and follow it. A new chat after install is the most reliable way for Cursor to auto-discover the skill.

Optional marketplace install (session-wide plugin, not this repo):

```text
/add-plugin superpowers
```

## Official sources

- Repository: https://github.com/obra/superpowers
- Brainstorming: https://github.com/obra/superpowers/blob/main/skills/brainstorming/SKILL.md
- Marketplace: https://github.com/obra/superpowers-marketplace
