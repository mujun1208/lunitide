import { useMemo, useState } from 'react';

type OfficeKind = 'ppt' | 'word' | 'excel';
type ViewMode = 'conversation' | 'preview' | 'review';

const tasks: Array<{ kind: OfficeKind; title: string; meta: string; state: string }> = [
  { kind: 'ppt', title: '董事会年度经营汇报', meta: 'PPT · 刚刚更新', state: '检查中' },
  { kind: 'word', title: '供应商评估报告', meta: 'Word · 昨天', state: '已验证' },
  { kind: 'excel', title: '区域销售经营分析', meta: 'Excel · 9月5日', state: '草稿' },
];

const kindMeta = {
  ppt: { label: '演示文稿', short: 'P', color: '#e9756c', file: '年度经营汇报_v3.pptx', count: '15 页' },
  word: { label: 'Word 文档', short: 'W', color: '#5b9cff', file: '供应商评估报告_v5.docx', count: '28 页' },
  excel: { label: 'Excel 工作簿', short: 'X', color: '#51c891', file: '区域销售分析_v2.xlsx', count: '6 个 Sheet' },
} as const;

const slides = [
  ['01', '年度经营回顾', '2026 BOARD REVIEW'],
  ['02', '核心结论', '增长延续，利润质量承压'],
  ['03', '收入稳健增长', '年度收入 2.3 亿元'],
  ['04', '增长结构', '区域与产品贡献'],
  ['05', '盈利质量', '毛利率同比 -2.4pt'],
  ['06', '现金流表现', '经营现金流改善'],
  ['07', '风险与应对', '三个关键风险'],
  ['08', '下一步决策', '预算与资源申请'],
];

function Icon({
  name,
}: {
  name: 'plus' | 'search' | 'file' | 'check' | 'history' | 'send' | 'back' | 'more' | 'download';
}) {
  const paths = {
    plus: <path d="M12 5v14M5 12h14" />,
    search: (
      <>
        <circle cx="11" cy="11" r="6" />
        <path d="m16 16 4 4" />
      </>
    ),
    file: (
      <>
        <path d="M7 3h7l4 4v14H7z" />
        <path d="M14 3v5h5" />
      </>
    ),
    check: <path d="m5 12 4 4L19 6" />,
    history: (
      <>
        <path d="M4 12a8 8 0 1 0 2.3-5.7L4 8" />
        <path d="M4 3v5h5M12 7v5l3 2" />
      </>
    ),
    send: (
      <>
        <path d="m4 4 17 8-17 8 3-8z" />
        <path d="M7 12h14" />
      </>
    ),
    back: <path d="m15 18-6-6 6-6" />,
    more: (
      <>
        <circle cx="5" cy="12" r="1" />
        <circle cx="12" cy="12" r="1" />
        <circle cx="19" cy="12" r="1" />
      </>
    ),
    download: (
      <>
        <path d="M12 3v12m0 0 4-4m-4 4-4-4" />
        <path d="M5 20h14" />
      </>
    ),
  };
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      {paths[name]}
    </svg>
  );
}

function DocumentCanvas({ kind, selectedSlide }: { kind: OfficeKind; selectedSlide: number }) {
  if (kind === 'word')
    return (
      <div className="word-page">
        <span className="doc-kicker">LUNITIDE RESEARCH</span>
        <h1>供应商评估与选型建议</h1>
        <p className="doc-subtitle">面向管理层的决策报告 · 2026 年 9 月</p>
        <div className="doc-rule" />
        <h2>执行摘要</h2>
        <p>本报告基于交付能力、总体成本、实施风险和长期服务四个维度，对入围供应商进行统一评估。</p>
        <blockquote>建议优先选择方案 B，并在合同中增加可用性目标与数据迁出条款。</blockquote>
        <h2>1. 评估结论</h2>
        <div className="doc-table">
          <b>评估维度</b>
          <b>方案 A</b>
          <b>方案 B</b>
          <span>综合得分</span>
          <span>82</span>
          <strong>91</strong>
          <span>实施风险</span>
          <span>中</span>
          <span>低</span>
        </div>
      </div>
    );
  if (kind === 'excel')
    return (
      <div className="excel-sheet">
        <div className="sheet-formula">
          <b>fx</b>
          <span>=SUMIFS(明细!$H:$H, 明细!$B:$B, B6)</span>
        </div>
        <div className="sheet-title">
          <small>FY2026 · SALES PERFORMANCE</small>
          <h1>区域销售经营概览</h1>
          <p>数据更新至 2026/08/31</p>
        </div>
        <div className="metric-grid">
          <div>
            <small>年度收入</small>
            <b>¥ 2.30 亿</b>
            <em>↑ 12.8%</em>
          </div>
          <div>
            <small>毛利率</small>
            <b>36.4%</b>
            <em className="down">↓ 2.4 pt</em>
          </div>
          <div>
            <small>目标达成</small>
            <b>94.7%</b>
            <em>↑ 3.1%</em>
          </div>
        </div>
        <div className="sheet-chart">
          <div className="bars">
            {[42, 55, 49, 68, 76, 71, 83, 91].map((v, i) => (
              <i key={i} style={{ height: `${v}%` }} />
            ))}
          </div>
          <div className="chart-legend">
            <span>1月</span>
            <span>4月</span>
            <span>8月</span>
          </div>
        </div>
        <div className="sheet-tabs">
          <b>经营概览</b>
          <span>区域分析</span>
          <span>产品分析</span>
          <span>原始数据</span>
        </div>
      </div>
    );
  const item = slides[selectedSlide] ?? slides[0];
  return (
    <div className="ppt-slide">
      <div className="slide-brand">
        <span>LUNITIDE</span>
        <b>{item[0]} / 15</b>
      </div>
      {selectedSlide === 0 ? (
        <div className="slide-cover">
          <small>2026 BOARD REVIEW</small>
          <h1>
            年度经营回顾
            <br />
            <em>与增长决策</em>
          </h1>
          <p>董事会经营汇报 · 2026 年 9 月</p>
        </div>
      ) : (
        <>
          <div className="slide-heading">
            <small>年度经营回顾</small>
            <h1>{item[1]}</h1>
            <p>{item[2]}</p>
          </div>
          <div className="slide-metrics">
            <div>
              <small>年度收入</small>
              <b>
                2.3<em>亿元</em>
              </b>
              <span>同比 +12.8%</span>
            </div>
            <div>
              <small>经营利润</small>
              <b>
                3,860<em>万元</em>
              </b>
              <span className="warn">同比 -4.2%</span>
            </div>
            <div>
              <small>现金回收</small>
              <b>
                96.1<em>%</em>
              </b>
              <span>同比 +3.6pt</span>
            </div>
          </div>
          <div className="slide-insight">
            <i />
            <p>
              <b>结论</b>增长趋势仍然稳健，但需要从追求规模转向提升结构质量。
            </p>
          </div>
        </>
      )}
    </div>
  );
}

export function OfficeStudioPreview(): React.JSX.Element {
  const [kind, setKind] = useState<OfficeKind>('ppt');
  const [mode, setMode] = useState<ViewMode>('preview');
  const [selectedSlide, setSelectedSlide] = useState(1);
  const [rightTab, setRightTab] = useState<'quality' | 'versions'>('quality');
  const [running, setRunning] = useState(true);
  const [notice, setNotice] = useState('');
  const meta = kindMeta[kind];
  const title = tasks.find((task) => task.kind === kind)?.title ?? tasks[0].title;
  const issues = useMemo(
    () =>
      kind === 'ppt'
        ? [
            { level: 'warning', title: '第 7 页信息密度偏高', note: '建议拆分风险与应对措施' },
            { level: 'info', title: '第 5 页数字来源已锁定', note: '来自 区域销售分析.xlsx / 经营概览' },
          ]
        : kind === 'word'
          ? [
              { level: 'info', title: '目录与标题层级一致', note: '共 4 级标题，未发现跳级' },
              { level: 'info', title: '3 条引用等待最终确认', note: '不阻断当前草稿预览' },
            ]
          : [
              { level: 'warning', title: '发现 1 个外部链接', note: '重算前需要确认是否断开' },
              { level: 'info', title: '关键汇总可回溯', note: '96 个公式通过引用检查' },
            ],
    [kind],
  );

  const chooseTask = (next: OfficeKind) => {
    setKind(next);
    setSelectedSlide(1);
    setMode('preview');
    setNotice('');
  };
  const toast = (message: string) => {
    setNotice(message);
    window.setTimeout(() => setNotice(''), 2200);
  };

  return (
    <div className="office-studio-shell">
      <aside className="studio-sidebar">
        <header>
          <button className="back-btn" aria-label="返回月汐">
            <Icon name="back" />
          </button>
          <span className="studio-moon" />
          <div>
            <b>Office Studio</b>
            <small>月汐办公专家</small>
          </div>
        </header>
        <button className="new-task" onClick={() => toast('新建任务入口将在正式版本连接对话')}>
          <Icon name="plus" /> 新建 Office 任务
        </button>
        <label className="task-search">
          <Icon name="search" />
          <input placeholder="搜索任务或文件" />
        </label>
        <div className="side-section-title">
          <span>最近任务</span>
          <button aria-label="更多任务">
            <Icon name="more" />
          </button>
        </div>
        <nav className="task-list">
          {tasks.map((task) => (
            <button
              key={task.kind}
              className={kind === task.kind ? 'active' : ''}
              onClick={() => chooseTask(task.kind)}
            >
              <i className={`file-mark ${task.kind}`}>{kindMeta[task.kind].short}</i>
              <span>
                <b>{task.title}</b>
                <small>{task.meta}</small>
              </span>
              <em>{task.state}</em>
            </button>
          ))}
        </nav>
        <div className="side-section-title files-title">
          <span>当前任务文件</span>
          <b>4</b>
        </div>
        <div className="source-files">
          <button>
            <Icon name="file" />
            <span>
              <b>年度经营报告.docx</b>
              <small>内容来源 · 8.4 MB</small>
            </span>
          </button>
          <button>
            <Icon name="file" />
            <span>
              <b>区域销售分析.xlsx</b>
              <small>数据来源 · 3.2 MB</small>
            </span>
          </button>
          <button>
            <Icon name="file" />
            <span>
              <b>公司演示模板.pptx</b>
              <small>品牌模板 · 12.1 MB</small>
            </span>
          </button>
        </div>
        <button className="drop-file" onClick={() => toast('可拖入 PPTX、DOCX、XLSX、PDF、图片等')}>
          <Icon name="plus" /> 添加材料或模板
        </button>
        <footer>
          <span className="engine-dot" />
          <span>
            <b>本地工作区</b>
            <small>所有文件已保存</small>
          </span>
          <button>
            <Icon name="more" />
          </button>
        </footer>
      </aside>

      <main className="studio-main">
        <header className="task-header">
          <div>
            <small>
              <span style={{ background: meta.color }}>{meta.short}</span>
              {meta.label} · {meta.count}
            </small>
            <h1>{title}</h1>
          </div>
          <div className="header-actions">
            <span className="verified">
              <Icon name="check" /> {running ? '正在检查' : '已验证'}
            </span>
            <button onClick={() => toast('已创建分享链接预览')}>分享</button>
            <button className="export-btn" onClick={() => toast(`${meta.file} 已准备导出`)}>
              <Icon name="download" /> 导出
            </button>
            <button aria-label="更多操作">
              <Icon name="more" />
            </button>
          </div>
        </header>
        <div className="studio-tabs">
          <nav>
            {(['conversation', 'preview', 'review'] as ViewMode[]).map((item) => (
              <button key={item} className={mode === item ? 'active' : ''} onClick={() => setMode(item)}>
                {item === 'conversation' ? '对话' : item === 'preview' ? '预览' : '检查'}
              </button>
            ))}
          </nav>
          <span>最后保存于 10:48</span>
          <div className="zoom">
            <button>−</button>
            <b>82%</b>
            <button>＋</button>
          </div>
        </div>

        <section className={`work-canvas mode-${mode}`}>
          {mode === 'conversation' ? (
            <div className="conversation-view">
              <div className="conversation-intro">
                <span style={{ background: meta.color }}>{meta.short}</span>
                <div>
                  <b>Office 办公专家</b>
                  <small>正在使用 {meta.label} 能力包</small>
                </div>
              </div>
              <div className="chat-row user">
                <p>把这份年度报告做成给董事会看的 15 页 PPT。使用公司模板，重点突出经营结果、风险和下一步决策。</p>
              </div>
              <div className="chat-row agent">
                <p>
                  我已完成材料和模板解析，并建立 15 页故事线。当前初稿已生成，正在检查页面密度、数字来源和模板一致性。
                </p>
                <div className="run-card">
                  <header>
                    <b>董事会年度经营汇报</b>
                    <span>执行中</span>
                  </header>
                  {['解析 3 份输入材料', '建立故事线与页面结构', '生成 PPTX 初稿', '检查页面与数字来源'].map(
                    (step, index) => (
                      <div key={step} className={index === 3 && running ? 'running' : 'done'}>
                        <i>{index === 3 && running ? '' : '✓'}</i>
                        <span>{step}</span>
                        {index === 3 && running && <em>8 / 15</em>}
                      </div>
                    ),
                  )}
                </div>
              </div>
            </div>
          ) : mode === 'review' ? (
            <div className="review-view">
              <header>
                <span className="quality-score">92</span>
                <div>
                  <h2>整体质量良好</h2>
                  <p>0 个阻断项 · 1 个建议 · 1 个信息</p>
                </div>
              </header>
              {issues.map((issue) => (
                <button key={issue.title} className={`review-row ${issue.level}`}>
                  <i />
                  <span>
                    <b>{issue.title}</b>
                    <small>{issue.note}</small>
                  </span>
                  <em>定位 ›</em>
                </button>
              ))}
            </div>
          ) : (
            <>
              {kind === 'ppt' && (
                <aside className="page-strip">
                  {slides.map((slide, index) => (
                    <button
                      key={slide[0]}
                      className={selectedSlide === index ? 'active' : ''}
                      onClick={() => setSelectedSlide(index)}
                    >
                      <small>{index + 1}</small>
                      <div>
                        <b>{slide[1]}</b>
                        <span>{slide[2]}</span>
                      </div>
                    </button>
                  ))}
                </aside>
              )}
              <div className="canvas-stage">
                <DocumentCanvas kind={kind} selectedSlide={selectedSlide} />
                <div className="canvas-caption">
                  <Icon name="check" />
                  <span>
                    <b>{meta.file}</b>
                    <small>{running ? '正在使用 Microsoft PowerPoint Renderer 检查' : '已完成结构与渲染检查'}</small>
                  </span>
                </div>
              </div>
            </>
          )}
        </section>

        <section className="studio-composer">
          <div className="selection-chip">
            <span style={{ background: meta.color }}>{meta.short}</span>
            {kind === 'ppt' ? `第 ${selectedSlide + 1} 页` : kind === 'word' ? '执行摘要' : '经营概览'}
            <button>×</button>
          </div>
          <textarea
            defaultValue={
              kind === 'ppt'
                ? '第 3 页的标题更结论化，并把三个指标的对比关系表达得更清楚。'
                : kind === 'word'
                  ? '把执行摘要压缩为一页，并保留当前管理层结论。'
                  : '增加区域目标达成率排名，不要修改原始数据。'
            }
          />
          <div className="composer-bar">
            <button className="attach">
              <Icon name="plus" />
            </button>
            <button>精确修改⌄</button>
            <span>将从当前版本创建新版本，其他内容保持不变</span>
            <button className="send" aria-label="发送修改要求" onClick={() => toast('修改要求已加入任务队列')}>
              <Icon name="send" />
            </button>
          </div>
        </section>
      </main>

      <aside className="studio-inspector">
        <header>
          <nav>
            <button className={rightTab === 'quality' ? 'active' : ''} onClick={() => setRightTab('quality')}>
              任务与检查
            </button>
            <button className={rightTab === 'versions' ? 'active' : ''} onClick={() => setRightTab('versions')}>
              版本
            </button>
          </nav>
          <button aria-label="收起检查栏">›</button>
        </header>
        {rightTab === 'quality' ? (
          <>
            <section className="progress-card">
              <div className="progress-head">
                <span className="pulse" />
                <div>
                  <b>{running ? '正在进行视觉检查' : '检查已完成'}</b>
                  <small>{running ? '第 8 / 15 页 · 约 1 分钟' : '全部页面与来源已检查'}</small>
                </div>
                <button onClick={() => setRunning(false)}>{running ? '停止' : '完成'}</button>
              </div>
              <div className="progress-bar">
                <i style={{ width: running ? '58%' : '100%' }} />
              </div>
              {['材料与模板解析', '故事线与页面结构', '文件生成', '渲染与质量检查'].map((step, index) => (
                <div className="step" key={step}>
                  <i className={index < 3 || !running ? 'done' : 'current'}>{index < 3 || !running ? '✓' : ''}</i>
                  <span>{step}</span>
                  <em>{index < 3 || !running ? '完成' : '进行中'}</em>
                </div>
              ))}
            </section>
            <section>
              <div className="inspector-title">
                <b>质量概览</b>
                <span>92 / 100</span>
              </div>
              <div className="quality-grid">
                <div>
                  <b>0</b>
                  <small>阻断</small>
                </div>
                <div>
                  <b>1</b>
                  <small>建议</small>
                </div>
                <div>
                  <b>14</b>
                  <small>已通过</small>
                </div>
              </div>
              {issues.map((issue) => (
                <button
                  key={issue.title}
                  className="issue-row"
                  onClick={() => {
                    setMode('review');
                    toast('已定位到相关内容');
                  }}
                >
                  <i className={issue.level} />
                  <span>
                    <b>{issue.title}</b>
                    <small>{issue.note}</small>
                  </span>
                  <em>›</em>
                </button>
              ))}
              <button className="all-results" onClick={() => setMode('review')}>
                查看全部检查结果
              </button>
            </section>
            <section>
              <div className="inspector-title">
                <b>数据与来源</b>
                <span>全部可追溯</span>
              </div>
              <div className="source-link">
                <i className="file-mark excel">X</i>
                <span>
                  <b>3 个关键指标</b>
                  <small>来自 区域销售分析.xlsx</small>
                </span>
                <Icon name="check" />
              </div>
              <div className="source-link">
                <i className="file-mark word">W</i>
                <span>
                  <b>7 条经营结论</b>
                  <small>来自 年度经营报告.docx</small>
                </span>
                <Icon name="check" />
              </div>
            </section>
          </>
        ) : (
          <section className="versions">
            <div className="inspector-title">
              <b>版本时间线</b>
              <button onClick={() => toast('正在比较 v2 与 v3')}>比较版本</button>
            </div>
            {[
              ['v3', '当前版本', '套用公司模板并修复布局', '10:48'],
              ['v2', '已验证', '修改第 3、5 页关键结论', '10:31'],
              ['v1', '初稿', '根据三份材料生成', '09:56'],
            ].map((version, index) => (
              <button className={index === 0 ? 'current' : ''} key={version[0]}>
                <i />
                <span>
                  <b>
                    {version[0]} · {version[1]}
                  </b>
                  <small>{version[2]}</small>
                  <em>{version[3]}</em>
                </span>
              </button>
            ))}
          </section>
        )}
        <footer>
          <button onClick={() => toast('已标记当前版本为接受')}>
            <Icon name="check" /> 接受当前版本
          </button>
          <button onClick={() => setRightTab('versions')}>
            <Icon name="history" /> 查看版本历史
          </button>
        </footer>
      </aside>
      {notice && (
        <div className="studio-toast" role="status">
          {notice}
        </div>
      )}
    </div>
  );
}
