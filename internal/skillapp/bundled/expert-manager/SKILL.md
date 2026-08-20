---
name: expert-manager
description: Create, refine, and publish six-section expert profiles for Lunitide. Use whenever the user wants to create a new expert, define an expert persona, convert industry experience into an expert, optimize an existing expert's six sections, or says 创建专家 / 新建专家 / expert-manager / 岗位说明书.
---

# Expert Manager

Guide the user from a rough idea ("XXX 专家，擅长 YYY，我的经验是…") to a validated **six-section expert profile** stored in Lunitide via `expert.create`.

## When this skill is active

The user is creating or reshaping an **expert** (岗位说明书式智能体), not a generic Skill. Experts have:

- **Frontmatter**: name, division, description, semver
- **Six sections** (all required, non-empty):
  1. **identity** — role, background, core strengths
  2. **mission** — goals and problems solved
  3. **rules** — behavioral constraints and boundaries
  4. **workflow** — standard steps when invoked
  5. **deliverableTemplate** — output format and structure
  6. **successMetrics** — how to judge good work

Valid divisions: `engineering`, `design`, `product`, `project-management`, `testing`, `security`, `operations`, `data`.

## Conversation flow

1. **Parse the opening prompt** — Extract expert domain (XXX), specialty (XXXXX), and user experience hints. Replace placeholders like `XXX` / `……` with concrete drafts; ask at most one clarifying question if critical facts are missing.

2. **Interview lightly** — Confirm: target users, typical tasks, tone (formal/casual), forbidden actions, and example deliverables. Prefer inferring from the user's experience paragraph before asking.

3. **Draft all six sections** — Write in Chinese unless the user uses English. Each section should be actionable prose (not bullet stubs). Align workflow with Lunitide project phases when relevant.

4. **Review with user** — Present a compact summary table of the six sections. Offer to revise before creation.

5. **Create via `expert.create`** — Call the tool once with:
   - `source`: `"local"`
   - `frontmatter`: `{ name, division, description, semver: "1.0.0" }`
   - `sixSection`: all six fields populated
   - `requestId`: new UUID

6. **After creation** — Tell the user the expert appears in 专家中心 and can be mounted to project phases (≤4 per phase).

## Quality bar

- Names: 1–128 chars, specific (e.g. 「数据库优化专家」 not 「专家」).
- Descriptions: 1–2000 chars, state scope and audience.
- Each section: 1–65536 chars; no placeholders like「待补充」in the final create call.
- Rules must include safety: no credential leakage, no destructive ops without confirmation.
- Deliverable template should show a concrete outline (headings / fields).

## Division hints

| User intent | division |
|-------------|----------|
| 代码、架构、DevOps | engineering |
| UI/UX、视觉 | design |
| 需求、路线图 | product |
| 排期、干系人 | project-management |
| QA、测试策略 | testing |
| 安全审计 | security |
| SRE、运维 | operations |
| 分析、BI | data |

## Do not

- Do not use `skill.create` for experts — use `expert.create` only.
- Do not skip any of the six sections.
- Do not publish without user confirmation when they only asked for a draft.
