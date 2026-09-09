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

3. **Draft all six sections** — Write in Chinese unless the user uses English. Each section should be actionable prose (not bullet stubs). Align workflow with Lunitide project phases when relevant. Infer from the user's brief; do not leave「待补充」.

4. **Match equipment** — From the injected `[可用技能目录]`, installed MCP presets, and built-in tools, decide what this expert actually needs:
   - Skills: published catalog keys the expert should invoke (e.g. `web-researcher`, `slide-builder`).
   - MCP: bind as `mcp:<presetId>` only when that server is listed/installed (e.g. `mcp:playwright`, `mcp:fetch`).
   - Capabilities: name the built-in tools the persona will rely on (`web.search`, `docx.gen`, `pptx.gen`, `computer.act`…). These stay in the six-section workflow; they are not `skillKeys`.
   - Skip unmatched items; do not invent unpublished skill names.

5. **Show the dossier in chat first** — This turn is not a 1–3 sentence operation receipt. Before or immediately after `expert.create`, output two GFM tables (not a prose dump, not only “去专家中心看”):
   - **岗位说明书**：`| 卡片 | 写入内容 |` covering 身份 / 使命 / 规则 / 流程 / 交付模板 / 成败标准 — each cell a real excerpt of what you wrote, not a stub.
   - **装备匹配**：`| 类型 | 名称 | 匹配理由 | 关联 |` with 类型 in 技能 / MCP / 能力. 关联 = 已写入 skillKeys / 仅写入流程 / 目录没有未挂.

6. **Create via `expert.create`** — Call the tool once with:
   - Flat fields: `name`, `division`, `description`, `semver: "1.0.0"`.
   - Flat six-section fields: `identity`, `mission`, `rules`, `workflow`, `deliverableTemplate`, `successMetrics`.
   - `skillKeys`: the matched published skills plus any `mcp:<id>` (and `brain:codex` / `brain:claude` only if the user asked for that brain).
   - Do not send `source`, `frontmatter`, `sixSection` or `requestId`; those belong to the native API, not this model tool.
   - Expert business topics such as novels, reports and Word output are profile content, not a request to generate a document now. Never substitute docx.gen for expert.create.

7. **After creation** — Keep the two tables on screen. Also report name, expertId, and that the profile stays **disabled** under 专家中心 > 我创建的. The user can open the card, trial without enabling, then enable before mounting to project phases (≤4 per phase). Never claim creation also enabled the expert. Do not replace the dossier with a one-line “已创建，请到专家中心”. Do not append generic「下一步建议」.

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
