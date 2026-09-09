# 社区技能来源与产品集成核验（2026-09-07）

本文件记录源码已核验且进入 Lunitide 技能市场的内容，不代表已安装、已发布或外部 CLI 已配置。遵照用户最新要求，本次不操作真实用户数据库、不替用户安装新增市场技能、不打包应用。

## 安装边界

- `skill-creator` 创建模板按已授权范围升级为 `2.0.0`，保留默认供应。其他原有已安装记录完整保留，社区新版由用户手动选择；不因同名目录更新重新安装已卸载的技能。同名同版本内容不同的草稿不会自动发布或覆盖。
- 其他 36 个社区入口及 6 个原生替代入口均为市场待安装，`Bundled=false`、`Compose=false`。用户测试时自行选择安装。
- 新增依赖入口用于完整工作流，例如 `grill-with-docs` 依赖 `grilling` 与 `domain-modeling`；依赖显示在清单中，但不以读取市场目录为理由自动安装。
- “Superpowers”和“gstack”是集合。前者按原技能目录提供入口；后者保留固定版本源码并提供路由入口，按任务读取单个工作流，不把全部内容灌入每轮对话。

## 固定来源

下列内容来自维护者仓库，用 Git HEAD 的完整 40 位提交锁定，再以该提交下载源码。没有执行下载的 setup、脚本、hook、依赖安装或后台服务。每个文件的原始字节、SHA-256 和长度记录在 `internal/skillapp/bundled/community/sources.json`，Git 对该目录关闭换行转换。

| 市场 ID / 技能名称 | 原始入口 | 许可依据 | 状态 |
| --- | --- | --- | --- |
| `skill-creator` / `skill-creator` | [anthropics/skills/skills/skill-creator/SKILL.md](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/skill-creator/SKILL.md) | [Apache-2.0](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/skill-creator/LICENSE.txt) | 既有内置升级 |
| `find-skills` / `find-skills` | [vercel-labs/skills/skills/find-skills/SKILL.md](https://github.com/vercel-labs/skills/blob/1682051d48c34f5eb135e6475c1a965dce05e820/skills/find-skills/SKILL.md) | [MIT](https://github.com/vercel-labs/skills/blob/1682051d48c34f5eb135e6475c1a965dce05e820/LICENSE) | 社区新版待安装；存量保留 |
| `frontend-design` / `frontend-design` | [anthropics/skills/skills/frontend-design/SKILL.md](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/frontend-design/SKILL.md) | [Apache-2.0](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/frontend-design/LICENSE.txt) | 社区新版待安装；存量保留 |
| `webapp-testing` / `webapp-testing` | [anthropics/skills/skills/webapp-testing/SKILL.md](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/webapp-testing/SKILL.md) | [Apache-2.0](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/webapp-testing/LICENSE.txt) | 市场待安装 |
| `web-artifacts-builder` / `web-artifacts-builder` | [anthropics/skills/skills/web-artifacts-builder/SKILL.md](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/web-artifacts-builder/SKILL.md) | [Apache-2.0](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/web-artifacts-builder/LICENSE.txt) | 市场待安装 |
| `brainstorming` / `brainstorming` | [obra/superpowers/skills/brainstorming/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/brainstorming/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 社区新版待安装；存量保留 |
| `systematic-debugging` / `systematic-debugging` | [obra/superpowers/skills/systematic-debugging/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/systematic-debugging/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `writing-plans` / `writing-plans` | [obra/superpowers/skills/writing-plans/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/writing-plans/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `executing-plans` / `executing-plans` | [obra/superpowers/skills/executing-plans/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/executing-plans/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `dispatching-parallel-agents` / `dispatching-parallel-agents` | [obra/superpowers/skills/dispatching-parallel-agents/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/dispatching-parallel-agents/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `subagent-driven-development` / `subagent-driven-development` | [obra/superpowers/skills/subagent-driven-development/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/subagent-driven-development/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `verification-before-completion` / `verification-before-completion` | [obra/superpowers/skills/verification-before-completion/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/verification-before-completion/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `requesting-code-review` / `requesting-code-review` | [obra/superpowers/skills/requesting-code-review/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/requesting-code-review/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `receiving-code-review` / `receiving-code-review` | [obra/superpowers/skills/receiving-code-review/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/receiving-code-review/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `test-driven-development` / `test-driven-development` | [obra/superpowers/skills/test-driven-development/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/test-driven-development/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `using-git-worktrees` / `using-git-worktrees` | [obra/superpowers/skills/using-git-worktrees/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/using-git-worktrees/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `finishing-a-development-branch` / `finishing-a-development-branch` | [obra/superpowers/skills/finishing-a-development-branch/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/finishing-a-development-branch/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `writing-skills` / `writing-skills` | [obra/superpowers/skills/writing-skills/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/writing-skills/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `superpowers` / `using-superpowers` | [obra/superpowers/skills/using-superpowers/SKILL.md](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/skills/using-superpowers/SKILL.md) | [MIT](https://github.com/obra/superpowers/blob/b36e0829c6d0140e93cfef2ca599b1b07d4a7797/LICENSE) | 市场待安装 |
| `gstack` / `gstack` | [garrytan/gstack/SKILL.md](https://github.com/garrytan/gstack/blob/0530392821c277b95e5cd65aa9d9fda4248718b2/SKILL.md) | [MIT](https://github.com/garrytan/gstack/blob/0530392821c277b95e5cd65aa9d9fda4248718b2/LICENSE) | 市场待安装 |
| `vercel-react-best-practices` / `vercel-react-best-practices` | [vercel-labs/agent-skills/skills/react-best-practices/SKILL.md](https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/skills/react-best-practices/SKILL.md) | [MIT](https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md) | 市场待安装 |
| `web-design-guidelines` / `web-design-guidelines` | [vercel-labs/agent-skills/skills/web-design-guidelines/SKILL.md](https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/skills/web-design-guidelines/SKILL.md) | [MIT](https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md) | 市场待安装 |
| `impeccable` / `impeccable` | [pbakaus/impeccable/.agents/skills/impeccable/SKILL.md](https://github.com/pbakaus/impeccable/blob/dbdc470e70dbbda69f9b78ee38bc38ea1d3560b9/.agents/skills/impeccable/SKILL.md) | [Apache-2.0](https://github.com/pbakaus/impeccable/blob/dbdc470e70dbbda69f9b78ee38bc38ea1d3560b9/LICENSE) | 市场待安装 |
| `design-taste-frontend` / `design-taste-frontend` | [Leonxlnx/taste-skill/skills/taste-skill/SKILL.md](https://github.com/Leonxlnx/taste-skill/blob/ccbc15639c97057cbfcf32ecebc38ef716e4bb37/skills/taste-skill/SKILL.md) | [MIT](https://github.com/Leonxlnx/taste-skill/blob/ccbc15639c97057cbfcf32ecebc38ef716e4bb37/LICENSE) | 市场待安装 |
| `code-review` / `code-review` | [mattpocock/skills/skills/engineering/code-review/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/code-review/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `tdd` / `tdd` | [mattpocock/skills/skills/engineering/tdd/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/tdd/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `matt-grill-me` / `grill-me` | [mattpocock/skills/skills/productivity/grill-me/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/productivity/grill-me/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `grill-with-docs` / `grill-with-docs` | [mattpocock/skills/skills/engineering/grill-with-docs/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/grill-with-docs/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `improve-codebase-architecture` / `improve-codebase-architecture` | [mattpocock/skills/skills/engineering/improve-codebase-architecture/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/improve-codebase-architecture/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `grilling` / `grilling` | [mattpocock/skills/skills/productivity/grilling/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/productivity/grilling/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `domain-modeling` / `domain-modeling` | [mattpocock/skills/skills/engineering/domain-modeling/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/domain-modeling/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `codebase-design` / `codebase-design` | [mattpocock/skills/skills/engineering/codebase-design/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/codebase-design/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `wayfinder` / `wayfinder` | [mattpocock/skills/skills/engineering/wayfinder/SKILL.md](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/wayfinder/SKILL.md) | [MIT](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE) | 市场待安装 |
| `agent-browser` / `agent-browser` | [vercel-labs/agent-browser/skill-data/core/SKILL.md](https://github.com/vercel-labs/agent-browser/blob/4726eceeb3274eef34ab082ee04d7288c54dec70/skill-data/core/SKILL.md) | [Apache-2.0](https://github.com/vercel-labs/agent-browser/blob/4726eceeb3274eef34ab082ee04d7288c54dec70/LICENSE) | 市场待安装 |
| `firecrawl` / `firecrawl` | [firecrawl/cli/skills/firecrawl/SKILL.md](https://github.com/firecrawl/cli/blob/06e2fd59d3a78051c8fcb05223b00aaff131267c/skills/firecrawl/SKILL.md) | [ISC](https://github.com/firecrawl/cli/blob/06e2fd59d3a78051c8fcb05223b00aaff131267c/package.json) | 市场待安装 |
| `deploy-checklist` / `deploy-checklist` | [anthropics/knowledge-work-plugins/engineering/skills/deploy-checklist/SKILL.md](https://github.com/anthropics/knowledge-work-plugins/blob/1f517b9de47e827c80cd933ed364e16838072239/engineering/skills/deploy-checklist/SKILL.md) | [Apache-2.0](https://github.com/anthropics/knowledge-work-plugins/blob/1f517b9de47e827c80cd933ed364e16838072239/LICENSE) | 市场待安装 |
| `incident-response` / `incident-response` | [anthropics/knowledge-work-plugins/engineering/skills/incident-response/SKILL.md](https://github.com/anthropics/knowledge-work-plugins/blob/1f517b9de47e827c80cd933ed364e16838072239/engineering/skills/incident-response/SKILL.md) | [Apache-2.0](https://github.com/anthropics/knowledge-work-plugins/blob/1f517b9de47e827c80cd933ed364e16838072239/LICENSE) | 市场待安装 |

## 明确的名称映射和不能混淆的来源

| 用户名称 | 实际处理 |
| --- | --- |
| code-review、tdd | Matt Pocock 的原始技能；原有 `tpl-code-reviewer`、`tpl-tdd-loop` 保留，不替换用户旧技能。 |
| grill-me | 新官方入口的市场 ID 为 `matt-grill-me`，技能名仍为 `grill-me`；旧 `grill-me` 模板 ID / `tpl-grill-me` 保留，避免旧引用失效。 |
| postmortem | 映射 Anthropic knowledge-work-plugins 的 `incident-response` 中 postmortem 模式，没有编造独立官方 `postmortem` 包。 |
| Design Taste | 映射明确可验证的 Leonxlnx/taste-skill 中 `design-taste-frontend`；不是声明所有同名社区项目等价。它面向 landing/portfolio/redesign，复杂业务 UI 可选择 Impeccable。 |
| Firecrawl | 来源为 Firecrawl 官方 CLI，非 Anthropic 自研技能。`package.json` 声明 ISC，连同原声明保存。托管 API 消耗 credits，有免费试用并不等于永久免费。 |
| agent-browser | 官方安装目录当前是 discovery stub；额外保留全部 `skill-data` 的实际工作流、命令参考和模板，避免仅安装一个让用户再找文档的壳。 |

## 不分发受限源码；提供原创能力入口

- Anthropic `docx`、`xlsx`、`pptx`、`pdf` 当前目录许可证明确限制复制、派生与第三方分发。该四项源代码没有进入产品目录。产品市场中的同名入口标注“Lunitide 原生”，使用项目已实现的 Office、docx.gen、excel.gen、pptx.gen、pdf.gen 能力；不是 Anthropic 官方源码安装。许可依据：[官方 docx LICENSE.txt](https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/docx/LICENSE.txt)，其余三目录同类许可。
- `doc-coauthoring` 当前原始目录没有明确的单项目许可文件，不能仅靠“多数技能开源”推断它获准再分发。本次提供 Lunitide 原创文档协作入口，标注 `upstream-license-unverified`，未复制该原文。
- `docker-optimize` 搜索可见同名候选，但当前源码真实性和分发许可未能一并确认，未冒认某维护者官方包。提供 Lunitide 原创 Dockerfile/compose 审查工作约定，并明确依赖本机 Docker 才能实际构建。

## 运行条件与能力边界

| 包 | 已保留内容 | 使用条件 |
| --- | --- | --- |
| skill-creator | 完整18个技能目录文件，以及仓库README/NOTICE。含 grader/comparator/analyzer、evals schema、viewer、aggregate/validate/package/run_eval/run_loop 脚本。 | 在 Lunitide 用 skill.create 保存草稿、skill.try 真实试用，再对照、改进、生成 Benchmark 报告，最后由技能中心发布。上游触发评估脚本要求 Claude Code CLI；没有配置时不可冒称跑过该脚本。 |
| Superpowers / Matt Pocock | 请求条目的完整目录和实际依赖技能。 | 替换上游工具名为当前 Lunitide 可用工具；实际并行依赖可用子智能体能力。上游其他助手配置、hooks 不自动启用。 |
| gstack | 固定提交的完整源码、65个SKILL.md、共享 bin/lib/browse/资源与各级许可。 | 原始可执行运行时需要 Bun、Bash 和构建后的 browse；未安装或验证时只称工作流资料可用。模型默认走 Lunitide 的实际工具，跳过 upstream setup/onboarding/telemetry/self-upgrade。目录符号链接保存为 `.symlink.txt`，不在用户机器创建链接。 |
| webapp-testing | Playwright 示例和 with_server.py。 | 原示例需要 Python + Playwright + 浏览器；也可通过现有 browser.act 验证，必须如实说明实际使用的方法。 |
| web-artifacts-builder | 原始初始化/打包脚本及 shadcn 资源归档。 | Node/包管理器与 Bash 需先检查。未执行原脚本，未在全局安装依赖。 |
| Impeccable | 通用 .agents 版完整 reference/agents/scripts 资源。 | 不自动配置其他产品 harness/CLI。页面范围按用户授权，黑白主题、小窗口与功能回归仍需实际验证。 |
| Firecrawl | 当前12个CLI技能及引用资料。 | API endpoint/key/credits是独立条件；不存在已配置证据时不调用付费服务或宣布已联网。免费现有 web/search/browser 能力保留。 |

## 完整性和验证

- 37 个社区包、1,915 个来源资源，约 32 MiB。未因提示词/Bridge大小限制裁剪源文件；例如 Design Taste 原文超过 80 KiB，清单只用短入口，完整源文件保留在包中供分段读取。
- `BundledPackageFiles` 校验来源标记、名称、版本、commit、整体摘要、来源路径/许可以及每文件SHA/长度；同名用户技能不能借名字取得官方资源。
- 资源物化使用产品不可覆盖目录。新增市场包不会在启动时自动安装。来源包不是新的执行授权，原业务权限和工具授权仍生效。
- 真实 SQLite 回归覆盖旧版本保留、同名同版本用户草稿不被覆盖/发布、升级重试不产生重复。完整资源回归覆盖跨平台路径、每文件完整性、实际支持文件、转发技能依赖和受限源码不分发。
- 本次测试通过：`go test ./internal/skillapp -count=1`。这里只声称目录、持久化和资源合同通过，未声称所有外部 CLI、网络服务或第三方脚本已真实运行。
