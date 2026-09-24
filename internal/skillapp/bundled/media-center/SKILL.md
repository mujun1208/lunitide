---
name: media-center
description: Play a public-domain movie or song inside Lunitide's own media center. Use when the user asks to find a film or song on the web and play it in 媒体中心. One media.play target=center call. Never another desktop player.
---

# 媒体中心播放

用户要在月汐自带的媒体中心里看到或听到片子、歌曲时，用这一条，不要去找电脑上的其它播放器。

## 怎么做

1. 立刻调用一次 `media.play`，不要先解释，也不要提到步数或额度。`target` 必须是 `center`。`query` 写用户说的片名或歌名；用户已经给了 mp4、webm 或 mp3 的 https 直链时，把地址放进 `url`。用户只说「找一部电影」时，`query` 就用这句话，工具会自己选一部公版片。
2. 工具返回 `MEDIA_CENTER` 后，用一句话告诉用户已经在媒体中心播放，然后停止。不要再调用 `web.search`、`web.fetch`、`computer.act`、`browser.act` 或 `desktop.open`。
3. 工具说明没有可直接播放的公版文件时，把这句话告诉用户。点名检索只查维基共享资源、NASA 和 Internet Archive 上标明公有领域或知识共享的直链。请对方给一个 https 直链，或在媒体中心选择本机文件。然后停止。

## 不要做

- 不要把媒体中心理解成汽水音乐、网易云或系统里的其它软件。
- 不要抓网页 HTML，不要下载商业片源，不要绕过加密。
- 没有直链就不要声称已经开始播放。
