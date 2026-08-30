---
name: computer-control
description: Operate this Windows PC with Lunitide computer.act. Use when the user asks to click, type, paste, press keys, switch or arrange windows, read the screen, or drive a desktop app. This PC only — never remote, LAN, or UAC.
---

# Computer Control

Drive the local Windows desktop the way Peekaboo / OpenClaw `computer.act` does: **see → act → verify**. The model-facing tool is `computer.act` (audit, rate limit, emergency stop, process blocklist). Do not invent a parallel stack (`command.run` SendKeys, Python, cua-driver, Peekaboo CLI). Do **not** call `cc.*` names — they are not in the tool list.

## When to Use

- Click, type, paste, press Enter/Tab, drag, scroll
- Switch, restore, move, resize, minimize, or close a **running** window
- Read buttons / edits via accessibility instead of guessing pixels
- Read or write the clipboard, then paste

Do **not** use this skill to: auto-click UAC / elevation / file Open-Save; launch apps that are not running (`desktop.open`); play music (`media.play`); browse the web (`browser.act`); kill OS processes.

## Loop

1. `computer.act` `action=list` (or `screenshot`) if you need a target.
2. `computer.act` `action=focus` so input lands in the right app.
3. `computer.act` `action=observe` (preferred) or `screenshot`. The observe image has Peekaboo-style badges (`B1`, `E1`) and a **`frameId`**. Node bounds are **image pixels**.
4. Act with `computer.act` (`click` / `type` / `press` / …). Pixel clicks/drags **must echo `frameId`**. Mismatch fails closed (`COMPUTER_STALE_FRAME`), including display reconnect / DPI / monitor count change. `frameId` ends with `sN` (`0` = virtual desktop, `1…` = monitor left-to-right).
5. If the tool returns a verify screenshot, check the hit before the next action. Unchanged pixels still attach the **current** frame and keep the same `frameId` — that id is still valid; do not reuse the pre-action image.
6. If the UI is animating, `computer.act` `action=wait` `until=change` (or a short timeout).

Mouse `x,y` / drag `x1,y1,x2,y2` **must** be pixels of the latest capture/observe image, not OS screen coordinates, plus that image's `frameId`. After a capture, window list bounds are those image pixels (`space=image`). Window move/resize still use OS screen pixels.

## Tool map

| Need | Tool |
| --- | --- |
| Unified act | `computer.act` `action=` + optional `frameId` / `screenIndex` |
| Launch not-running | `desktop.open` |
| Type after a label | `desktop.type` |
| Play music | `media.play` |
| Web pages | `browser.act` |

## Hard refusals

- UAC (`consent.exe`) / elevation: observe marks them refused; **never** confirm or click.
- File Open/Save: listed with `needs_user` — tell the user to click 保存/打开/取消. Do not auto-click.
- Do not close/hide/quit `explorer.exe`, `lunitide.exe`, or other protected shell processes.
- Do not `TerminateProcess`; quit is window close only.
- This PC only. No LAN, remote desktop, or BeeBEEP.

## After each action

Speak a short Chinese status (what you did, what you see). If blocked (risk / confirm / emergency stop), tell the user what to enable or confirm in 设置 → 电脑控制. Do not retry a blocked critical combo.
