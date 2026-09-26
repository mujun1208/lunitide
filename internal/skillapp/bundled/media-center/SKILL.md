---
name: media-center
description: Play a public-domain movie or song inside Lunitide's own media center. Use when the user asks to find a film or song on the web and play it in 媒体中心. One media.play target=center call. Never another desktop player.
---

# 媒体中心播放

用户要在月汐自带的媒体中心里看到或听到片子、歌曲时，用这一条，不要去找电脑上的其它播放器。

## 怎么做

1. 立刻调用一次 `media.play`，不要先解释，也不要提到步数或额度。`target` 必须是 `center`。`query` 写用户说的片名或歌名；用户已经给了 mp4、webm、m3u8 或 mp3 的 https 直链时，把地址放进 `url`。用户只说「找一部电影」时，`query` 就用这句话，工具会自己选一部公版片。
2. 工具返回 `MEDIA_CENTER` 后，用一句话告诉用户已经在媒体中心播放（返回里有 `site` 时说明来自哪个网站，如南瓜影视、酷我音乐），然后停止。不要再调用 `web.search`、`web.fetch`、`computer.act`、`browser.act` 或 `desktop.open`。
3. 公版库里没有时，工具会自动从对接的免费网站（电影：南瓜影视；音乐：酷我音乐）提取真实播放直链，照样在媒体中心直接播放，返回的还是 `MEDIA_CENTER`。
4. 工具报错说没有，就是网上也没有：照实告诉用户没有这部片子/这首歌，然后停止。不要打开网页，不要换一部播放，也不要自己去抓网站的视频地址。

## 不要做

- 不要把媒体中心理解成汽水音乐、网易云或系统里的其它软件。
- 不要抓网页 HTML，不要下载商业片源，不要绕过加密。
- 工具没有返回 `MEDIA_CENTER` 就不要声称已经开始播放。
- 不要打开爱奇艺、优酷、网易云这些会员网页。
