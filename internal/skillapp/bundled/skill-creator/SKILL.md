---
name: skill-creator
description: Create new skills, modify and improve existing skills. Use when users want to create a skill from scratch, edit an existing skill, or optimize a skill's description for better triggering.
---

# Skill Creator

Help the user turn an idea into a Lunitide skill that is stored with `skill.create`. Do not invent a second runtime. Do not run Claude Code evals, Python review scripts, or any path that is not on this machine.

## What a skill is here

A Lunitide skill is:

- Frontmatter: `name` (slug), `displayName`, `description`, `version`
- Permissions: one or more of `read_only`, `read_write`, `network`, `file_system`, `shell`, `admin`
- `entryPoint`: `SKILL.md` or `builtin://…`
- `manifestJson`: JSON with `triggers` (keywords) and `prompt` (working agreement the model sees on invoke)

The skill is **prompt + triggers**, executed by `skill.invoke`. There is no `eval-viewer`, no `claude-with-access-to-the-skill`, and no background benchmark runner.

## Flow

1. Parse what the skill should do and when it should trigger.
2. If a folder with `SKILL.md` already exists, `workspace.list` then `workspace.read` each file. Do not stop after listing.
3. Otherwise draft in the session workspace: `workspace.write` a `SKILL.md` and, if needed, a short `manifest.json`.
4. Review the draft with the user in one short table (name, triggers, permissions, what tools it may call).
5. Call `skill.create` once with `name`, `displayName`, `description`, `permissions`, `entryPoint`, `manifestJson`.
6. Tell the user in Chinese: 已创建「名称」，请到技能中心安装并发布. Then continue remaining work.

If `skill.create` fails (duplicate name, bad JSON, permissions), say why in Chinese. Do not pretend it succeeded.

## Drafting rules

- Write the prompt in the user's language. Be actionable; do not dump a textbook.
- Triggers must be words a user would actually say.
- Only request permissions the skill needs. Default `read_write`. Add `network` only if it must search or fetch. Add `shell` only if it must `command.run`.
- Point the prompt at existing Lunitide tools (`web.search`, `browser.act`, `computer.act`, `docx.gen`, `excel.gen`, `workspace.edit`). Do not tell the model to call Python, Office COM, or `cc.*` names.
- After create, use `skill.view` if you need to confirm the stored body.

## Editing an existing skill

Use `skill.view` to read it. Propose a patched prompt. Call `skill.create` only for a new slug, or tell the user to edit in Skill Center if they asked to overwrite.

## What you must never do

- Do not run `eval-viewer/generate_review.py` or any Claude evaluation harness.
- Do not tell the user the skill was benchmarked.
- Do not use `plugin.create` for a skill.
- Do not finish after only listing a directory of SKILL.md files.
