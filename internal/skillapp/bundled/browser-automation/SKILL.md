---
name: browser-automation
description: Drive this PC's managed browser with browser.act. Use for filling forms, scraping public pages, clicking through a site, and extracting structured data. Not for desktop apps (cc.*) or remote machines.
---

# Browser Automation

OpenClaw-style loop on **this PC only**: **snapshot → act → resnapshot**. One managed Playwright browser. Do not guess CSS. Do not drive the user's daily Chrome profile unless they chose that mode in 设置 → 浏览器.

## When to Use

- Fill a public web form, click through a wizard, scrape a page
- Extract fields into `structured.output` then `excel.gen` / `docx.gen`

Do **not** use this skill to: operate desktop apps (`computer-control` / `cc.*`); play music (`media.play`); confirm UAC or file Open/Save; log into sites by guessing 2FA/captcha.

## Loop

1. `browser.act` `op=navigate` with an absolute `https` URL. Prefer the snapshot it returns; do not open a second tab for the same task.
2. If you have no refs, `op=snapshot`.
3. `click` / `type` using snapshot refs (or a selector only when the snapshot named it). After the tool returns, use the appended `[snapshot after …]` tree — do not reuse old refs.
4. If a ref is stale, snapshot **once** and retry that single action. If it is still stale, stop and describe the blocker.
5. Login wall, 2FA, captcha, camera/mic permission, or a file picker: tell the user what to do. Do not invent credentials or click through security prompts.
6. For page text without clicking, `op=read` (fetch). For controls, stay on snapshot/act.

## After the page

Speak a short Chinese status. If the user asked for a spreadsheet or JSON, call `structured.output` then `excel.gen` / `docx.gen`. Do not dump an unchecked code fence as the deliverable.
