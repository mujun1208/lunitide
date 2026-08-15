from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote
import re

root = Path(__file__).resolve().parent
html_files = sorted(root.rglob("*.html"))
assert len(html_files) == 65, f"expected 65 HTML files, got {len(html_files)}"
errors = []
refs = 0

class Parser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.links = []
        self.ids = []
    def handle_starttag(self, tag, attrs):
        values = dict(attrs)
        if values.get("id"):
            self.ids.append(values["id"])
        if tag in {"a", "link", "script"}:
            attr = "href" if tag in {"a", "link"} else "src"
            if values.get(attr):
                self.links.append(values[attr])

for page in html_files:
    parser = Parser()
    source = page.read_text(encoding="utf-8")
    try:
        parser.feed(source)
    except Exception as exc:
        errors.append(f"parse {page.relative_to(root)}: {exc}")
        continue
    duplicate_ids = sorted({item for item in parser.ids if parser.ids.count(item) > 1})
    if duplicate_ids:
        errors.append(f"duplicate ids {page.relative_to(root)}: {', '.join(duplicate_ids)}")
    main_open = len(re.findall(r"<main\b", source, re.IGNORECASE))
    main_close = len(re.findall(r"</main\s*>", source, re.IGNORECASE))
    if main_open != main_close:
        errors.append(f"unbalanced main {page.relative_to(root)}: {main_open} open / {main_close} close")
    if main_open > 1 and page.name != "06-完整UI界面设计.html":
        errors.append(f"multiple main landmarks {page.relative_to(root)}: {main_open}")
    if main_open == 1 and "<footer" in source.lower():
        if source.lower().rfind("</main>") > source.lower().find("<footer"):
            errors.append(f"footer inside main {page.relative_to(root)}")
    for raw in parser.links:
        refs += 1
        base, separator, fragment_raw = raw.partition("#")
        target = unquote(base.split("?", 1)[0])
        fragment = unquote(fragment_raw.split("?", 1)[0]) if separator else ""
        if target.startswith(("http://", "https://", "mailto:", "javascript:")):
            continue
        path = page if not target else (page.parent / target).resolve()
        if not path.exists():
            errors.append(f"broken {page.relative_to(root)} -> {raw}")
            continue
        if fragment and path.suffix.lower() == ".html":
            target_parser = Parser()
            target_parser.feed(path.read_text(encoding="utf-8"))
            if fragment not in target_parser.ids:
                errors.append(f"broken anchor {page.relative_to(root)} -> {raw}")

blueprint = (root / "06-完整UI界面设计.html").read_text(encoding="utf-8")
required = [
    "需求架构规范", "方案和 UI 设计", "数据库", "接口", "开发", "测试", "集成", "发布",
    "项目清单", "技能中心", "资产管理", "设置",
    "预览", "浏览器", "CLI", "文件管理", "计划",
    "九阶段交付控制台", "V2.0 全功能界面图谱",
    "WebView2 缺失", "Provider", "上下文检查器", "ToolCall 审批",
    "恢复中心", "扩展生态目录", "委派树与 Barrier", "CR 修订",
    "记忆收件箱", "组织治理界面族",
    "文件与附件", "ChangeSet", "测试与证据", "发布包", "模板管理子视图",
    "月伴全屏对话画板", "M9.5"
]
for item in required:
    if item not in blueprint:
        errors.append(f"blueprint missing: {item}")

contract_markers = {
    "02-全局架构与安全基线.html": [
        "bridge-envelope-contract", "consent-snapshot-egress", "logging-retention-baseline"
    ],
    "M2/02-技术设计.html": ["m2-settings-domain-model", "m2-atomic-save-apply-recovery"],
    "M6/02-技术设计.html": ["m6-s5-data-skill-contract", "PaginationSpec", "McpTool", "resourceLimits"],
    "M7/02-技术设计.html": ["legacy-domain-release-contracts", "ReleaseMemberDigest", "parallel_with"],
    "M8/02-技术设计.html": ["m8-tech-ontology-legacy", "Versioned Index", "episodic"],
    "M9.5/02-技术设计.html": ["m95-fsm", "internal/tts/sapi"],
}
for relative, markers in contract_markers.items():
    content = (root / relative).read_text(encoding="utf-8")
    for marker in markers:
        if marker not in content:
            errors.append(f"migration contract missing: {relative} -> {marker}")

for asset in (root / "assets/style.css", root / "assets/site.js"):
    if not asset.exists() or asset.stat().st_size == 0:
        errors.append(f"missing asset: {asset.name}")

if errors:
    print("FAIL")
    print("\n".join(errors))
    raise SystemExit(1)
print(f"PASS: {len(html_files)} HTML files, {refs} references, 0 broken local links")
print("PASS: reference UI, workspace tabs, asset management, and M0-M9.5 screen families present")
