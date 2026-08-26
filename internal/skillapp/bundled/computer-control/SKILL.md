---
name: computer-control
description: Operate this Windows PC with Lunitide cc.* tools. Use when the user asks to click, type, paste, press keys, switch or arrange windows, read the screen, or drive a desktop app. This PC only — never remote, LAN, or UAC.
---

# Computer Control

Drive the local Windows desktop the way Peekaboo / OpenClaw `computer.act` does: **see → act → verify**. All actions go through governed `cc.*` tools (audit, rate limit, emergency stop, process blocklist). Do not invent a parallel stack (`command.run` SendKeys, Python, cua-driver, Peekaboo CLI).

## When to Use

- Click, type, paste, press Enter/Tab, drag, scroll
- Switch, restore, move, resize, minimize, or close a **running** window
- Read buttons / edits via accessibility instead of guessing pixels
- Read or write the clipboard, then paste

Do **not** use this skill to: auto-click UAC / elevation / file Open-Save; launch apps that are not running (`desktop.open`); play music (`media.play`); browse the web (`browser.act`); kill OS processes.

## Loop

1. `cc.window_list` or `cc.get_active_window` if you need a target.
2. `cc.window_focus` (title/process fragment or `0xHWND`) so input lands in the right app.
3. `cc.observe_ui` (preferred) or `cc.screen_capture`. The observe image has Peekaboo-style badges (`B1`, `E1`). Node bounds are **image pixels**.
4. Act with the matching tool (below).
5. If the tool returns a verify screenshot, check the hit before the next action. Unchanged pixels mean the click likely missed — observe again, do not blindly repeat.
6. If the UI is animating, `cc.wait` `until=change` (or a short timeout).

Mouse `x,y` / drag `x1,y1,x2,y2` **must** be pixels of the latest capture/observe image, not OS screen coordinates. After a capture, `cc.window_list` bounds are those image pixels (`space=image`). `cc.window_action` move/resize still use OS screen pixels.

## Tool map

| Need | Tool |
| --- | --- |
| Screenshot | `cc.screen_capture` (`desktop` / `foreground` / `window`+`title`) |
| Named controls + badges | `cc.observe_ui` then `cc.mouse_click` `id=B1` or `name=` |
| Click / scroll | `cc.mouse_click` (`scroll`, `scrollAxis=horizontal`) |
| Drag | `cc.mouse_drag` |
| Type Unicode | `cc.keyboard_type` — **focus the app first** (`cc.window_focus` or `window=`) |
| Chord | `cc.keyboard_shortcut` (`ctrl+s`; reserved combos refused) |
| Single key | `cc.press` (`enter`, `tab`, `esc`; `count` 1–8) |
| Paste | `cc.paste` (`text` optional — sets clipboard then Ctrl+V) |
| Clipboard only | `cc.clipboard` `get`/`set` — Unicode text only, capped at 8192, no files |
| Dialog Yes/OK | `cc.observe_dialog` then `cc.confirm_dialog` |
| Menu | `cc.menu_click` path `File > Save` or `文件/保存` |
| Fill an edit | `cc.set_value` `target=` name or observe id |
| List / focus windows | `cc.window_list`, `cc.window_focus` |
| Min/max/restore/move/resize/hide/close | `cc.window_action` `op=` |
| Running apps | `cc.app_list`; quit with `cc.app_quit` (WM_CLOSE only) |
| Launch not-running | `desktop.open` — never duplicate with `cc.*` |

## Hard refusals

- UAC (`consent.exe`), elevation, file Open/Save: `cc.observe_ui` / `cc.observe_dialog` mark them refused; **never** confirm or click.
- Do not close/hide/quit `explorer.exe`, `lunitide.exe`, or other protected shell processes.
- Do not `TerminateProcess`; quit is window close only.
- This PC only. No LAN, remote desktop, or BeeBEEP.

## After each action

Speak a short Chinese status (what you did, what you see). If blocked (risk / confirm / emergency stop), tell the user what to enable or confirm in 设置 → 电脑控制. Do not retry a blocked critical combo.
