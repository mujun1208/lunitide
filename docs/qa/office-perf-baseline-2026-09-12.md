# 办公内核计时基线（2026-09-12）

来源：`go test ./internal/officestudio -run TestMeasureOfficeFixturesPrintsBaseline -v`  
机器：开发工作区单次（n=1），**不是**冻结门槛。`SummarizePerf.Ready=false`。

建议门槛（未冻结）：Generate P50 ≤ 8s / P95 ≤ 20s；Check(false) P50 ≤ 3s / P95 ≤ 8s；单节点 Patch+无渲染检查 P50 ≤ 4s。

| 夹具 | 操作 | 本次 ms |
|---|---|---|
| 12-page-ppt | generate | 32 |
| 12-page-ppt | check | 7 |
| 12-page-ppt | patch | 31 |
| 20-block-word | generate | 1 |
| 20-block-word | check | 12 |
| 20-block-word | patch | 3 |
| 3-sheet-excel | generate | 10 |
| 3-sheet-excel | check | 0 |
| short-pdf | generate | 90 |
| short-pdf | check | 0 |

不含模型思考时间。独立 PDF 本次走 gofpdf。Patch 只对 PPT/Word 做单文本节点 + 无渲染检查。n≥30 后才可把数字写进门禁 assert。
