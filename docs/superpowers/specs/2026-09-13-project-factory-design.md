# 项目管理工厂升级 · 设计指针

**日期：** 2026-09-13  
**状态：** 待评审。批准前不写实施计划、不改业务代码。  
**分类：** Architectural  
**唯一事实源（合同正文）：** [../../design/PRD-project-factory-2026-09-13.md](../../design/PRD-project-factory-2026-09-13.md)  
**不得推翻：** [../../design/PRD-project-workbench-spine-2026-09-13.md](../../design/PRD-project-workbench-spine-2026-09-13.md)

本文不是第二份合同。实施、验收、错误码、题库、模式、统计公式一律以工厂 PRD 为准。下面只固定结构，防止把总 PRD 再拆成互相打架的短文。

## 1 在现码上长什么

```
已有脊椎 F0
  根 / 树 / 开发清单 / 三执行器 / 测试退回开发
        ↓
F1 生成 + 规范     projectgen + projectrules + deliverable.draft
F2 库表物化         projectschema（仅 SQLite）
F3 工作台内核       扩展 projecttask → 接口 + 开发统计/再处理
F4 测试 + 集成      projecttestkit + 双回退 + 场景成员
F5 整树包 + 同步    release.sync
```

接口 / 开发 / 测试 / 集成共用 `WorkBoardV1`，不接 `m6_integration`。

## 2 切片

只按 PRD §9 的 F1→F5 依次写 writing-plans。禁止一份计划覆盖五片。

## 3 评审时若要改口

先改工厂 PRD，再改实施计划。常见改口入口已写在 PRD §0.2（方案 10 份、SQLite、本机同步）。
