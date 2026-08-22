---
name: ui-components
description: Build polished UIs with high-quality component patterns — shadcn/ui, Radix primitives, Tailwind. Use for marketing pages, dashboards, and multi-component React/Vue apps.
---

# UI Components (High-Quality Reference)

Prefer battle-tested component patterns over one-off markup.

## Default Stack (when not constrained)

- React + TypeScript
- Tailwind CSS with design tokens
- shadcn/ui + Radix UI for accessible primitives
- Vite for dev/build

## Component Selection

Before coding, pick components that match the job:

| Need | Prefer |
|------|--------|
| Forms | Input, Select, Checkbox, RadioGroup, Form + zod |
| Overlays | Dialog, Sheet, Popover, DropdownMenu |
| Data | Table, DataTable pattern, Tabs |
| Feedback | Toast, Alert, Skeleton, Progress |
| Navigation | Sidebar, Breadcrumb, Command palette |

Reference: https://ui.shadcn.com/docs/components

## Implementation Rules

1. Compose shadcn patterns; customize tokens, not fork internals blindly
2. Keep spacing on a 4px grid; consistent radii and shadows
3. States: default, hover, focus-visible, disabled, loading, error
4. Responsive: mobile-first; test at 375px and 1280px
5. Accessibility: labels, focus rings, keyboard paths — no div-only buttons

## Avoid AI Slop

- No purple-gradient hero on white for every page
- No wall of identical rounded cards without hierarchy
- No Inter + center-align everything template

Pair with `frontend-design` for art direction and `design-system` for cross-page consistency.
