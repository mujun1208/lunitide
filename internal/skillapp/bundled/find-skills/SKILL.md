---
name: find-skills
description: Helps users discover and install agent skills when they ask how to do X, want to find a skill, or need to extend capabilities. Use when the user is looking for functionality that might exist as an installable skill.
---

# Find Skills

Help users discover skills already shipped with Lunitide or installable from the built-in skill market.

## When to Use

Use when the user:

- Asks "how do I do X" where X might have an existing skill
- Says "find a skill for X" or "有没有技能可以…"
- Wants to extend agent capabilities without writing prompts from scratch
- Is unsure which skill fits their task (product, design, coding, docs, etc.)

## Step 1: Clarify the Need

Identify:

1. Domain (React, testing, design, PM, deployment, writing, etc.)
2. Specific task (write PRD, polish UI, split tickets, review code)
3. Whether a bundled or market template likely exists

## Step 2: Search Lunitide Skill Catalog (Primary)

1. Call `skill.catalog.list` (or guide the user to **技能中心 → 市场**).
2. Match by triggers, display name, description, and category.
3. Prefer these bundled essentials when relevant:
   - `skill-creator` — create or refine custom skills
   - `brainstorming` — explore product/design before building
   - `pm-skill` — PRD, persona, business model, research
   - `super-coders` — split tasks and drive implementation
   - `frontend-design` — distinctive, production-grade UI
   - `ui-components` — shadcn/ui and high-quality component patterns
   - `design-system` — keep visual language consistent across pages

## Step 3: Present Options

For each match, explain in Chinese:

1. What the skill does
2. When to use it vs alternatives
3. How to install: `skill.install({ templateId })` then publish in Skill Center

Ask which one to install; do not install without confirmation unless the user already asked to install.

## Step 4: Install and Verify

1. `skill.install({ templateId: "<id>" })`
2. `skill.publish({ id: "<skillId>" })` if still draft
3. Confirm with `skill.list` that status is `published`

## Step 5: If Nothing Fits

1. Say no good built-in match was found
2. Offer to help directly or use `skill-creator` to author a new skill
3. Optionally use `web.search` for public skill ideas — do not blindly run external CLIs

## Tips

- Prefer Chinese display names when talking to the user
- One recommendation first, then 2–3 alternatives
- Never claim a skill is installed until publish succeeded
