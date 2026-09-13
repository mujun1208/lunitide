#!/usr/bin/env python3
"""Vendor pinned community skills into internal/skillapp/bundled/community."""

from __future__ import annotations

import hashlib
import json
import re
import tempfile
import urllib.request
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT / "internal" / "skillapp" / "bundled" / "community"
SOURCES = DEST / "sources.json"
UA = "lunitide-community-vendor/1.0"

SKIP_DIR = {".git", "__pycache__", "node_modules", ".venv", "engine", "proxy", "rewriter", "browse", "mcp", "shrink", "dist"}
SKIP_FILE_SUFFIX = {".zip", ".exe", ".dll", ".so", ".dylib", ".skill"}

PACKAGES = [
    {
        "id": "mcp-builder",
        "catalogId": "mcp-builder",
        "name": "mcp-builder",
        "repository": "https://github.com/anthropics/skills",
        "commit": "41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f",
        "subdirectory": "skills/mcp-builder",
        "license": "Apache-2.0",
        "licenseEvidence": "https://github.com/anthropics/skills/blob/41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f/skills/mcp-builder/LICENSE.txt",
        "category": "研发效能",
        "aliases": ["MCP", "mcp-builder"],
        "dependencies": ["Python 或 Node MCP SDK（按任务核验）"],
        "runtimeNotes": [
            "用Lunitide现有workspace/command写MCP服务；上游脚本依赖外部SDK，未安装不得宣称已创建可运行服务。",
        ],
        "extras": ["README.md", "THIRD_PARTY_NOTICES.md"],
    },
    {
        "id": "vercel-optimize",
        "catalogId": "vercel-optimize",
        "name": "vercel-optimize",
        "repository": "https://github.com/vercel-labs/agent-skills",
        "commit": "063bee94c3f4df8453406c830b0a7df0f2860278",
        "subdirectory": "skills/vercel-optimize",
        "license": "MIT",
        "licenseEvidence": "https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md",
        "category": "研发效能",
        "aliases": ["Vercel优化"],
        "dependencies": ["Vercel项目与已授权的部署指标（可选）"],
        "runtimeNotes": ["没有Vercel指标时只审查仓库配置，不编造节省金额。"],
        "extras": ["README.md"],
    },
    {
        "id": "writing-guidelines",
        "catalogId": "writing-guidelines",
        "name": "writing-guidelines",
        "repository": "https://github.com/vercel-labs/agent-skills",
        "commit": "063bee94c3f4df8453406c830b0a7df0f2860278",
        "subdirectory": "skills/writing-guidelines",
        "license": "MIT",
        "licenseEvidence": "https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md",
        "category": "审美设计",
        "aliases": ["文案规范"],
        "dependencies": [],
        "runtimeNotes": ["按当前产品文案与真实页面审查，不套用未核验的外部站点。"],
        "extras": ["README.md"],
    },
    {
        "id": "composition-patterns",
        "catalogId": "composition-patterns",
        "name": "composition-patterns",
        "repository": "https://github.com/vercel-labs/agent-skills",
        "commit": "063bee94c3f4df8453406c830b0a7df0f2860278",
        "subdirectory": "skills/composition-patterns",
        "license": "MIT",
        "licenseEvidence": "https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md",
        "category": "研发效能",
        "aliases": ["组合模式"],
        "dependencies": [],
        "runtimeNotes": ["对照当前React源码给改造建议，不虚构已完成的重构。"],
        "extras": ["README.md"],
    },
    {
        "id": "react-view-transitions",
        "catalogId": "react-view-transitions",
        "name": "react-view-transitions",
        "repository": "https://github.com/vercel-labs/agent-skills",
        "commit": "063bee94c3f4df8453406c830b0a7df0f2860278",
        "subdirectory": "skills/react-view-transitions",
        "license": "MIT",
        "licenseEvidence": "https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md",
        "category": "审美设计",
        "aliases": ["View Transitions"],
        "dependencies": [],
        "runtimeNotes": ["未运行浏览器核验时标明过渡效果未实测。"],
        "extras": ["README.md"],
    },
    {
        "id": "react-native-skills",
        "catalogId": "react-native-skills",
        "name": "react-native-skills",
        "repository": "https://github.com/vercel-labs/agent-skills",
        "commit": "063bee94c3f4df8453406c830b0a7df0f2860278",
        "subdirectory": "skills/react-native-skills",
        "license": "MIT",
        "licenseEvidence": "https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md",
        "category": "研发效能",
        "aliases": ["React Native"],
        "dependencies": [],
        "runtimeNotes": ["按当前RN项目结构工作；缺少模拟器时不宣称已跑通。"],
        "extras": ["README.md"],
    },
    {
        "id": "deploy-to-vercel",
        "catalogId": "deploy-to-vercel",
        "name": "deploy-to-vercel",
        "repository": "https://github.com/vercel-labs/agent-skills",
        "commit": "063bee94c3f4df8453406c830b0a7df0f2860278",
        "subdirectory": "skills/deploy-to-vercel",
        "license": "MIT",
        "licenseEvidence": "https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/README.md",
        "category": "研发效能",
        "aliases": ["部署到Vercel"],
        "dependencies": ["Vercel CLI或已授权的部署账号（可选）"],
        "runtimeNotes": ["没有用户授权和凭证时只准备清单，不代为部署或声明已上线。"],
        "extras": ["README.md"],
    },
    {
        "id": "matt-handoff",
        "catalogId": "handoff",
        "name": "handoff",
        "repository": "https://github.com/mattpocock/skills",
        "commit": "3cca18b368ae95cdbdebbff572ccafa662551015",
        "subdirectory": "skills/productivity/handoff",
        "license": "MIT",
        "licenseEvidence": "https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/LICENSE",
        "category": "研发效能",
        "aliases": ["会话交接"],
        "dependencies": [],
        "runtimeNotes": ["交接材料只写本会话真实进度与未决问题，不编造下游已读回执。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "caveman",
        "catalogId": "caveman",
        "name": "caveman",
        "repository": "https://github.com/JuliusBrussee/caveman",
        "commit": "15581d14007fd01fb3f132016741962f34936ca2",
        "subdirectory": "skills/caveman",
        "license": "MIT",
        "licenseEvidence": "https://github.com/JuliusBrussee/caveman/blob/15581d14007fd01fb3f132016741962f34936ca2/LICENSE",
        "category": "研发效能",
        "aliases": ["caveman mode", "少token"],
        "dependencies": [],
        "runtimeNotes": [
            "只分发MIT许可的skills/caveman说明。不复制或不执行Engine-linked/BSL目录。",
            "安全警告与不可逆操作仍用完整句子，不因压缩漏掉风险。",
        ],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "codeql",
        "catalogId": "codeql",
        "name": "codeql",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/static-analysis/skills/codeql",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["CodeQL"],
        "dependencies": ["CodeQL CLI（可选，未安装则只写查询计划）"],
        "runtimeNotes": ["仅用于已授权仓库的防御性静态分析。未运行CodeQL不得宣称已发现漏洞。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "semgrep",
        "catalogId": "semgrep",
        "name": "semgrep",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/static-analysis/skills/semgrep",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["Semgrep"],
        "dependencies": ["Semgrep CLI（可选）"],
        "runtimeNotes": ["仅用于已授权仓库的防御性规则审查。未运行不得宣称扫描通过。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "sarif-parsing",
        "catalogId": "sarif-parsing",
        "name": "sarif-parsing",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/static-analysis/skills/sarif-parsing",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["SARIF"],
        "dependencies": [],
        "runtimeNotes": ["只解析用户提供的真实SARIF文件，不编造finding。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "variant-analysis",
        "catalogId": "variant-analysis",
        "name": "variant-analysis",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/variant-analysis",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["变体分析"],
        "dependencies": [],
        "runtimeNotes": ["在已授权代码中查找同类缺陷模式。每个候选必须落到真实文件行，禁止编造利用链。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "supply-chain-risk-auditor",
        "catalogId": "supply-chain-risk-auditor",
        "name": "supply-chain-risk-auditor",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/supply-chain-risk-auditor",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["供应链审计"],
        "dependencies": [],
        "runtimeNotes": ["对照真实lockfile。缺少advisory数据就标未知，不编造CVE。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "modern-python",
        "catalogId": "modern-python",
        "name": "modern-python",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/modern-python",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["现代Python"],
        "dependencies": [],
        "runtimeNotes": ["按当前仓库工具链给建议，不替用户全局安装包管理器。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "modern-cpp",
        "catalogId": "modern-cpp",
        "name": "modern-cpp",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/modern-cpp",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["现代C++"],
        "dependencies": [],
        "runtimeNotes": ["对照真实C++源码。未编译通过不得宣称现代化完成。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "rust-review",
        "catalogId": "rust-review",
        "name": "rust-review",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/rust-review",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["Rust审查"],
        "dependencies": [],
        "runtimeNotes": ["安全审查只针对已授权代码。发现需落到文件与理由，禁止编写利用PoC。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "property-based-testing",
        "catalogId": "property-based-testing",
        "name": "property-based-testing",
        "repository": "https://github.com/trailofbits/skills",
        "commit": "321ccfe628eca0d314b0ee4eaffcdd8a05639aaf",
        "subdirectory": "plugins/property-based-testing",
        "license": "CC-BY-SA-4.0",
        "licenseEvidence": "https://github.com/trailofbits/skills/blob/321ccfe628eca0d314b0ee4eaffcdd8a05639aaf/LICENSE",
        "category": "研发效能",
        "aliases": ["属性测试"],
        "dependencies": ["Hypothesis / fast-check / proptest（按语言核验）"],
        "runtimeNotes": ["未实际跑测试时标记未知，不编造通过率。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "playwright-skill",
        "catalogId": "playwright-skill",
        "name": "playwright-skill",
        "repository": "https://github.com/LambdaTest/agent-skills",
        "commit": "0de6ebfd44b7c67c62171320c4446f13856fc443",
        "subdirectory": "playwright-skill",
        "license": "MIT",
        "licenseEvidence": "https://github.com/LambdaTest/agent-skills/blob/0de6ebfd44b7c67c62171320c4446f13856fc443/LICENSE",
        "category": "研发效能",
        "aliases": ["Playwright"],
        "dependencies": ["Playwright（可选）；TestMu云为独立账号"],
        "runtimeNotes": ["优先本地或现有browser.act。没有云凭证时不调用TestMu，不宣称云上已跑。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "cypress-skill",
        "catalogId": "cypress-skill",
        "name": "cypress-skill",
        "repository": "https://github.com/LambdaTest/agent-skills",
        "commit": "0de6ebfd44b7c67c62171320c4446f13856fc443",
        "subdirectory": "cypress-skill",
        "license": "MIT",
        "licenseEvidence": "https://github.com/LambdaTest/agent-skills/blob/0de6ebfd44b7c67c62171320c4446f13856fc443/LICENSE",
        "category": "研发效能",
        "aliases": ["Cypress"],
        "dependencies": ["Cypress（可选）"],
        "runtimeNotes": ["没有云凭证时只写本地测试。未运行不得宣称通过。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "jest-skill",
        "catalogId": "jest-skill",
        "name": "jest-skill",
        "repository": "https://github.com/LambdaTest/agent-skills",
        "commit": "0de6ebfd44b7c67c62171320c4446f13856fc443",
        "subdirectory": "jest-skill",
        "license": "MIT",
        "licenseEvidence": "https://github.com/LambdaTest/agent-skills/blob/0de6ebfd44b7c67c62171320c4446f13856fc443/LICENSE",
        "category": "研发效能",
        "aliases": ["Jest"],
        "dependencies": ["Jest（项目已有时）"],
        "runtimeNotes": ["测试命令以当前仓库脚本为准。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "vitest-skill",
        "catalogId": "vitest-skill",
        "name": "vitest-skill",
        "repository": "https://github.com/LambdaTest/agent-skills",
        "commit": "0de6ebfd44b7c67c62171320c4446f13856fc443",
        "subdirectory": "vitest-skill",
        "license": "MIT",
        "licenseEvidence": "https://github.com/LambdaTest/agent-skills/blob/0de6ebfd44b7c67c62171320c4446f13856fc443/LICENSE",
        "category": "研发效能",
        "aliases": ["Vitest"],
        "dependencies": ["Vitest（项目已有时）"],
        "runtimeNotes": ["测试命令以当前仓库脚本为准。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "pytest-skill",
        "catalogId": "pytest-skill",
        "name": "pytest-skill",
        "repository": "https://github.com/LambdaTest/agent-skills",
        "commit": "0de6ebfd44b7c67c62171320c4446f13856fc443",
        "subdirectory": "pytest-skill",
        "license": "MIT",
        "licenseEvidence": "https://github.com/LambdaTest/agent-skills/blob/0de6ebfd44b7c67c62171320c4446f13856fc443/LICENSE",
        "category": "研发效能",
        "aliases": ["pytest"],
        "dependencies": ["pytest（可选）"],
        "runtimeNotes": ["未运行pytest时标记未知。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "selenium-skill",
        "catalogId": "selenium-skill",
        "name": "selenium-skill",
        "repository": "https://github.com/LambdaTest/agent-skills",
        "commit": "0de6ebfd44b7c67c62171320c4446f13856fc443",
        "subdirectory": "selenium-skill",
        "license": "MIT",
        "licenseEvidence": "https://github.com/LambdaTest/agent-skills/blob/0de6ebfd44b7c67c62171320c4446f13856fc443/LICENSE",
        "category": "研发效能",
        "aliases": ["Selenium"],
        "dependencies": ["Selenium（可选）"],
        "runtimeNotes": ["没有云凭证时只写本地或现有browser流程。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "cicd-pipeline-skill",
        "catalogId": "cicd-pipeline-skill",
        "name": "cicd-pipeline-skill",
        "repository": "https://github.com/LambdaTest/agent-skills",
        "commit": "0de6ebfd44b7c67c62171320c4446f13856fc443",
        "subdirectory": "cicd-pipeline-skill",
        "license": "MIT",
        "licenseEvidence": "https://github.com/LambdaTest/agent-skills/blob/0de6ebfd44b7c67c62171320c4446f13856fc443/LICENSE",
        "category": "研发效能",
        "aliases": ["CI/CD测试"],
        "dependencies": [],
        "runtimeNotes": ["对照仓库真实CI文件。未触发流水线不得宣称已绿。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "duckdb-attach-db",
        "catalogId": "attach-db",
        "name": "attach-db",
        "repository": "https://github.com/duckdb/duckdb-skills",
        "commit": "7feda8e01e22bc0886c86123f3884947e36d8c69",
        "subdirectory": "skills/attach-db",
        "license": "MIT",
        "licenseEvidence": "https://github.com/duckdb/duckdb-skills/blob/7feda8e01e22bc0886c86123f3884947e36d8c69/LICENSE",
        "category": "研发效能",
        "aliases": ["DuckDB挂载"],
        "dependencies": ["duckdb CLI"],
        "runtimeNotes": ["本机未安装duckdb时只准备SQL，不假装已查询。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "duckdb-query",
        "catalogId": "duckdb-query",
        "name": "query",
        "repository": "https://github.com/duckdb/duckdb-skills",
        "commit": "7feda8e01e22bc0886c86123f3884947e36d8c69",
        "subdirectory": "skills/query",
        "license": "MIT",
        "licenseEvidence": "https://github.com/duckdb/duckdb-skills/blob/7feda8e01e22bc0886c86123f3884947e36d8c69/LICENSE",
        "category": "研发效能",
        "aliases": ["DuckDB查询"],
        "dependencies": ["duckdb CLI", "attach-db或可读文件"],
        "runtimeNotes": ["只报告真实查询输出。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "duckdb-read-file",
        "catalogId": "duckdb-read-file",
        "name": "read-file",
        "repository": "https://github.com/duckdb/duckdb-skills",
        "commit": "7feda8e01e22bc0886c86123f3884947e36d8c69",
        "subdirectory": "skills/read-file",
        "license": "MIT",
        "licenseEvidence": "https://github.com/duckdb/duckdb-skills/blob/7feda8e01e22bc0886c86123f3884947e36d8c69/LICENSE",
        "category": "研发效能",
        "aliases": ["DuckDB读文件"],
        "dependencies": ["duckdb CLI"],
        "runtimeNotes": ["远程S3/GCS需要用户已有凭证。没有凭证就停在本地文件。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "duckdb-docs",
        "catalogId": "duckdb-docs",
        "name": "duckdb-docs",
        "repository": "https://github.com/duckdb/duckdb-skills",
        "commit": "7feda8e01e22bc0886c86123f3884947e36d8c69",
        "subdirectory": "skills/duckdb-docs",
        "license": "MIT",
        "licenseEvidence": "https://github.com/duckdb/duckdb-skills/blob/7feda8e01e22bc0886c86123f3884947e36d8c69/LICENSE",
        "category": "研发效能",
        "aliases": ["DuckDB文档"],
        "dependencies": [],
        "runtimeNotes": ["文档检索失败时标明未知，不编造SQL方言。"],
        "extras": ["LICENSE", "README.md"],
    },
    {
        "id": "awesome-agent-skills",
        "catalogId": "awesome-agent-skills",
        "name": "awesome-agent-skills",
        "repository": "https://github.com/VoltAgent/awesome-agent-skills",
        "commit": "8873794bcb26ff5dcf9cd518c87cf5638ca44b92",
        "subdirectory": "",
        "entry": "upstream/README.md",
        "license": "MIT",
        "licenseEvidence": "https://github.com/VoltAgent/awesome-agent-skills/blob/8873794bcb26ff5dcf9cd518c87cf5638ca44b92/LICENSE",
        "category": "研发效能",
        "aliases": ["技能索引", "awesome skills"],
        "dependencies": [],
        "runtimeNotes": [
            "这是发现索引，不是1000个技能的源码包。匹配后走skill.catalog.list与skill.install。",
            "npx skills或其他宿主安装结果无效。",
        ],
        "extras": ["LICENSE", "README.md"],
        "description": "Curated index of official and community agent skills. Use to discover a skill, then install it through Lunitide catalog — not by vendoring the whole list.",
    },
    {
        "id": "skill-doctor",
        "catalogId": "skill-doctor",
        "name": "skill-doctor",
        "repository": "https://github.com/alirezarezvani/claude-skills",
        "commit": "19392f7a08264ed00486a251f5b2098321771f94",
        "subdirectory": "engineering/skill-doctor",
        "license": "MIT",
        "licenseEvidence": "https://github.com/alirezarezvani/claude-skills/blob/19392f7a08264ed00486a251f5b2098321771f94/LICENSE",
        "category": "研发效能",
        "aliases": ["技能审计", "skill auditor"],
        "dependencies": [],
        "runtimeNotes": [
            "只审计用户指定的技能文件。388项工程库未整包分发。",
            "审计结论必须引用真实文件，不把外部榜单当已安装证据。",
        ],
        "extras": ["LICENSE", "README.md"],
    },
]


def download_zip(repo: str, commit: str) -> Path:
    slug = repo.removeprefix("https://github.com/")
    url = f"https://codeload.github.com/{slug}/zip/{commit}"
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    cache = Path(tempfile.gettempdir()) / "lunitide-skill-zips"
    cache.mkdir(parents=True, exist_ok=True)
    dest = cache / f"{slug.replace('/', '_')}__{commit}.zip"
    if dest.exists() and dest.stat().st_size > 1000:
        return dest
    print(f"download {url}", flush=True)
    with urllib.request.urlopen(req, timeout=120) as resp, dest.open("wb") as out:
        out.write(resp.read())
    return dest


def extract_root(zf: zipfile.ZipFile) -> str:
    names = zf.namelist()
    if not names:
        raise RuntimeError("empty zip")
    return names[0].split("/")[0]


def should_skip(rel: str) -> bool:
    parts = Path(rel).parts
    if any(p in SKIP_DIR for p in parts):
        return True
    name = Path(rel).name
    if name.startswith(".") and name not in {".gitattributes"}:
        if name in {".gitignore"}:
            return True
    suffix = Path(rel).suffix.lower()
    return suffix in SKIP_FILE_SUFFIX


def copy_file(src: bytes, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.write_bytes(src)


def sha256_bytes(b: bytes) -> str:
    return hashlib.sha256(b).hexdigest()


def parse_frontmatter(text: str) -> dict:
    if not text.startswith("---"):
        return {}
    end = text.find("\n---", 3)
    if end < 0:
        return {}
    block = text[3:end]
    data = {}
    key = None
    buf = []
    for line in block.splitlines():
        if re.match(r"^[A-Za-z0-9_-]+:", line):
            if key:
                data[key] = "\n".join(buf).strip().strip("\"'")
            key, _, rest = line.partition(":")
            key = key.strip()
            rest = rest.strip()
            if rest in {">", "|"}:
                buf = []
            else:
                buf = [rest.strip("\"'")]
        elif key and (line.startswith("  ") or line.startswith("\t") or line.startswith(" ")):
            buf.append(line.strip())
    if key:
        data[key] = "\n".join(buf).strip().strip("\"'")
    return data


def find_skill_md(files: dict[str, bytes], subdirectory: str) -> str:
    prefix = subdirectory.strip("/")
    candidates = []
    for path in files:
        if path.endswith("/SKILL.md") or path == "SKILL.md":
            rel = path
            if not prefix or rel == prefix + "/SKILL.md" or rel.startswith(prefix + "/"):
                candidates.append(rel)
    if prefix and f"{prefix}/SKILL.md" in files:
        return f"{prefix}/SKILL.md"
    skill_root = [p for p in candidates if p.endswith("SKILL.md")]
    if not skill_root:
        raise FileNotFoundError(f"SKILL.md missing under {subdirectory or '.'}")
    skill_root.sort(key=lambda p: (p.count("/"), len(p)))
    return skill_root[0]


def collect_from_zip(zf: zipfile.ZipFile, root: str, spec: dict) -> dict[str, bytes]:
    out = {}
    extras = spec.get("extras") or []
    sub = spec.get("subdirectory") or ""
    for info in zf.infolist():
        if info.is_dir():
            continue
        name = info.filename
        if not name.startswith(root + "/"):
            continue
        rel = name[len(root) + 1 :]
        if not rel or should_skip(rel):
            continue
        keep = rel in extras
        if sub:
            keep = keep or rel == sub or rel.startswith(sub + "/")
        else:
            keep = keep or rel in extras
        if not keep:
            continue
        if rel.endswith(".zip"):
            continue
        data = zf.read(info)
        out[rel.replace("\\", "/")] = data
    return out


def write_package(spec: dict, files: dict[str, bytes]) -> dict:
    pkg_id = spec["id"]
    pkg_dir = DEST / pkg_id
    if pkg_dir.exists():
        for old in pkg_dir.rglob("*"):
            if old.is_file():
                old.unlink()
    resources = []
    for rel in sorted(files):
        dest_rel = f"upstream/{rel}"
        dest = pkg_dir / Path(*dest_rel.split("/"))
        copy_file(files[rel], dest)
        resources.append({"path": dest_rel.replace("\\", "/"), "sha256": sha256_bytes(files[rel]), "bytes": len(files[rel])})
    entry = spec.get("entry")
    if not entry:
        skill = find_skill_md(files, spec.get("subdirectory") or "")
        entry = "upstream/" + skill
    raw = files.get(entry.removeprefix("upstream/"), b"")
    meta = parse_frontmatter(raw.decode("utf-8", errors="replace")) if raw else {}
    description = spec.get("description") or meta.get("description") or spec["name"]
    digest_h = hashlib.sha256()
    for resource in sorted(resources, key=lambda r: r["path"]):
        digest_h.update(f"{resource['path']}\x00{resource['sha256']}\n".encode())
    return {
        "id": spec["id"],
        "catalogId": spec["catalogId"],
        "name": spec["name"],
        "version": "2.0.0",
        "repository": spec["repository"],
        "commit": spec["commit"],
        "subdirectory": spec.get("subdirectory") or "",
        "entry": entry,
        "license": spec["license"],
        "licenseEvidence": spec["licenseEvidence"],
        "description": description,
        "category": spec["category"],
        "aliases": spec.get("aliases") or [],
        "dependencies": spec.get("dependencies") or [],
        "runtimeNotes": spec.get("runtimeNotes") or [],
        "digest": digest_h.hexdigest(),
        "resources": resources,
    }


def main() -> None:
    existing = json.loads(SOURCES.read_text(encoding="utf-8"))
    by_id = {p["id"]: i for i, p in enumerate(existing)}
    zips: dict[tuple[str, str], zipfile.ZipFile] = {}
    try:
        for spec in PACKAGES:
            key = (spec["repository"], spec["commit"])
            if key not in zips:
                zpath = download_zip(spec["repository"], spec["commit"])
                zips[key] = zipfile.ZipFile(zpath)
            zf = zips[key]
            root = extract_root(zf)
            files = collect_from_zip(zf, root, spec)
            if spec.get("subdirectory") and not any(
                p == spec["subdirectory"] or p.startswith(spec["subdirectory"] + "/") for p in files
            ):
                raise FileNotFoundError(f"{spec['id']}: missing {spec['subdirectory']}")
            if not spec.get("entry") and spec.get("subdirectory"):
                find_skill_md(files, spec["subdirectory"])
            elif spec.get("entry"):
                rel = spec["entry"].removeprefix("upstream/")
                if rel not in files:
                    raise FileNotFoundError(f"{spec['id']}: missing {rel}")
            receipt = write_package(spec, files)
            if spec["id"] in by_id:
                existing[by_id[spec["id"]]] = receipt
            else:
                by_id[spec["id"]] = len(existing)
                existing.append(receipt)
            print(f"vendored {spec['id']} resources={len(receipt['resources'])}", flush=True)
    finally:
        for zf in zips.values():
            zf.close()
    SOURCES.write_text(json.dumps(existing, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {SOURCES} packages={len(existing)}")


if __name__ == "__main__":
    main()
