# Work / AgentHub / Workspace / Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 按 0→1→2→3→4 把说话月、工作区简约壳、AgentHub 对话化、Work/AgentHub 壳层、路由管理+OCR 全部落地。

**Architecture:** 共享 Composer 挂 Work 与 AgentHub；工作区统一 chrome；路由从供应商页拆出；OCR 本机引擎可装可选。

**Tech Stack:** React/TypeScript, Vitest, Go engine, existing agentHub/ocrapp bridges.

**Spec:** [docs/superpowers/specs/2026-09-15-work-agenthub-workspace-routing-design.md](../specs/2026-09-15-work-agenthub-workspace-routing-design.md)

## Global Constraints

- TDD：先红后绿。
- 不提交用户未点名的无关改动。
- PP-OCR 未装不可选；不宣称随包。
- 不把 SessionPage 整页嵌进 AgentHub。

## File ownership

| Track | Files |
|---|---|
| 0 moon | `web/src/session/companion/visual/Strands.tsx`, `moonVisual.test.ts` |
| 1 workspace | `web/src/workspace/Workspace.tsx`, `CodePanel.tsx`, `Workspace.test.tsx`, `SessionPage.tsx`, `styles.css` |
| 2 AgentHub | `web/src/agentHub/*`, `LaunchSidebar.tsx`, `App.tsx` |
| 3 shell | `AgentHubShellSwitch.tsx`, `App.tsx`, `styles.css` |
| 4 routing | `settingsNav.ts`, `SettingsPage.tsx`, `OCRRouting.tsx`, `internal/ocrapp/*` |

## Tasks

1. Workspace chrome + expand on all tabs + slim browser/files/code.
2. SessionPage wires Workspace expand.
3. Settings `routing` category; OCR local engines + PP-OCR install/detect.
4. Shell Work/AgentHub + small drawer toggle.
5. AgentHub sidebar lights/connect/history; Work-like composer; agent title.
6. Verify targeted tests.
