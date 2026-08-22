---
name: design-system
description: Keep visual language consistent across pages and features — tokens, typography, spacing, components. Use for multi-page apps, refactors, or when UI feels inconsistent.
---

# Design System Consistency

Ensure every screen feels like the same product.

## Step 1 — Audit

Read existing styles (CSS variables, Tailwind config, theme files, component library):

- Color tokens (background, surface, text, accent, semantic)
- Typography scale (display, title, body, caption, mono)
- Spacing scale (4/8/12/16/24/32…)
- Radius, shadow, border rules
- Motion duration/easing

List inconsistencies: duplicate hex values, one-off margins, mixed radii.

## Step 2 — Define or Extend Tokens

If missing, propose a minimal token set in `:root` or `tailwind.config`:

```css
--color-bg, --color-surface, --color-text, --color-muted, --color-accent
--font-display, --font-body, --font-mono
--space-1 … --space-8
--radius-sm, --radius-md, --radius-lg
```

Document in `docs/design-system.md` or project README section.

## Step 3 — Component Contracts

For each shared component (Button, Card, Input, Nav):

- Variants (primary/secondary/ghost/danger)
- Sizes (sm/md/lg)
- Allowed combinations only — no ad-hoc styles in pages

## Step 4 — Migrate & Guard

- Refactor pages to tokens; delete magic numbers
- New UI must import shared components — flag exceptions
- Pair with `frontend-design` for new screens, not to override tokens randomly

## Output

Deliver: token table, component usage notes, and a short checklist for future PRs.
