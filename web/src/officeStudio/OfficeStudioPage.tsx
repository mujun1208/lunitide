import {OfficeChartDialog,type OfficeChartTarget} from './OfficeChartDialog';
import {OfficeSheetChartDialog,type OfficeSheetChartTarget} from './OfficeSheetChartDialog';
import {OfficeStorageDialog} from './OfficeStorageDialog';
import {OfficeImageDialog,type OfficeImageTarget,type OfficeImageUploader} from './OfficeImageDialog';
import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, RefreshCw } from 'lucide-react';
import { Dialog } from '../ui/Dialog';
import { listActiveSessionIds, subscribeLiveChatRegistry } from '../session/liveChat';
import { OFFICE_ARTIFACT_FOCUS_KEY, OFFICE_STUDIO_HOME_EVENT, requestedOfficeTask } from './officeNavigation';
import { officeRead } from './officeRead';
import { officeStudioUserError } from './officeUserError';
import { useLanguage } from '../i18n/language';
import { OfficeArtifactViewer } from './OfficeArtifactViewer';
import { OfficeMetricPanel } from './OfficeMetricPanel';
import { OfficeBundleDialog } from './OfficeBundleDialog';
import { OfficeDiffDialog } from './OfficeDiffDialog';
import { validateOfficeFiles, type OfficeImportRevision } from './officeUpload';
import {
  OfficeChecks,
  OfficeSources,
  OfficeVersions,
  officeInspectorLabels,
  type OfficeInspectorTab,
} from './OfficeInspector';
import {
  officeStudioApi,
  type OfficeNode,
  type OfficePreview,
  type OfficeRendererStatus,
  type OfficeStudioApi,
  type OfficeTask,
  type OfficeTaskDetail,
  type OfficeVersion,
} from './officeStudioApi';
import { defaultOfficeArtifact, isOfficeReference, officeDate, officeRunLabel } from './officePresentation';
import './officeStudio.css';
import { useOfficePanelResize } from './useOfficePanelResize';
import { OfficeReferences } from './OfficeReferences';

export interface OfficeConversationOptions {
  initialPrompt?: string;
  onReady: () => void;
  onActivityChange: (active: boolean) => void;
}
interface Props {
  api?: OfficeStudioApi;
  initialTaskId?: string;
  renderConversation: (task: OfficeTask, options: OfficeConversationOptions) => React.ReactNode;
  onOpenSession: (task: OfficeTask) => Promise<void> | void;
  onImportFiles?: (
    task: OfficeTask,
    files: File[],
    progress: (text: string) => void,
    signal: AbortSignal,
    revision?: OfficeImportRevision,
  ) => Promise<void>;
  onUploadImage?: OfficeImageUploader;
  onOpenExport?: (task: OfficeTask, path: string, reveal: boolean) => Promise<void>;
}
const LAST_TASK = 'lunitide:office-studio:last-task';
const message = (error: unknown): string => officeStudioUserError(error, '操作没有完成，请重试。');
const SAVED_READ_NOTICE = '操作已保存，最新记录暂时读不到。当前保留上次已读内容，请重新读取完整记录，无需再次提交。';
function visibleDetail(previous: OfficeTaskDetail | undefined, next: OfficeTaskDetail): OfficeTaskDetail {
  if (!next.snapshotIncomplete || previous?.task.id !== next.task.id) return next;
  return {
    ...previous,
    task: next.taskSnapshotStale ? previous.task : next.task,
    committed: next.committed,
    snapshotIncomplete: true,
    taskSnapshotStale: true,
    loadNotice: next.loadNotice || SAVED_READ_NOTICE,
  };
}

export function OfficeStudioPage({
  api = officeStudioApi,
  initialTaskId,
  renderConversation,
  onOpenSession,
  onImportFiles,
  onUploadImage,
  onOpenExport,
}: Props): React.JSX.Element {
  const zh = useLanguage() === 'zh-CN';
  const [tasks, setTasks] = useState<OfficeTask[]>([]);
  const [taskId, setTaskId] = useState(() => initialTaskId ?? requestedOfficeTask());
  const [taskLoading, setTaskLoading] = useState(false);
  const [taskLoadError, setTaskLoadError] = useState('');
  const [taskLoadRevision, setTaskLoadRevision] = useState(0);
  const [listError, setListError] = useState('');
  const panelResize = useOfficePanelResize(taskId);
  const [detail, setDetail] = useState<OfficeTaskDetail>();
  const [artifactId, setArtifactId] = useState('');
  const [versionId, setVersionId] = useState('');
  const [preview, setPreview] = useState<OfficePreview>();
  const [previewError, setPreviewError] = useState('');
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewRevision, setPreviewRevision] = useState(0);
  const [previewPage, setPreviewPage] = useState<{ versionId: string; offset: number; previous: number[] }>({
    versionId: '',
    offset: 0,
    previous: [],
  });
  const [selection, setSelection] = useState<{ versionId: string; offset: number; node: OfficeNode }>();
  const [tab, setTab] = useState<OfficeInspectorTab | null>('conversation');
  const [filesOpen, setFilesOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [goal, setGoal] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const snapshotIncomplete = !!detail?.snapshotIncomplete;
  const mutationBusy = busy || snapshotIncomplete;
  const [activity, setActivity] = useState(false);
  const [checking, setChecking] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [initialFiles, setInitialFiles] = useState<File[]>([]);
  const [renameOpen, setRenameOpen] = useState(false);
  const [titleDraft, setTitleDraft] = useState('');
  const [goalDraft, setGoalDraft] = useState('');
  const [nativeTarget, setNativeTarget] = useState<{taskId:string;artifactId:string;versionId:string;expectedRevision:number;versionNo:number;kind:'docx'|'xlsx'}>();
  const [chartTarget, setChartTarget] = useState<OfficeChartTarget>();
  const [sheetChartTarget, setSheetChartTarget] = useState<OfficeSheetChartTarget>();
  const [nativeStopRequested, setNativeStopRequested] = useState(false);
  const nativeGeneration = useRef(0);
  const [storageOpen, setStorageOpen] = useState(false);
  const [imageTarget, setImageTarget] = useState<OfficeImageTarget>();
  const [patchOpen, setPatchOpen] = useState(false);
  const [patchText, setPatchText] = useState('');
  const [patchTarget, setPatchTarget] = useState<{
    taskId: string;
    artifactId: string;
    baseVersionId: string;
    expectedRevision: number;
    nodeId: string;
    nodeDigest: string;
    label: string;
    versionNo: number;
    originalText: string;
  }>();
  const [exportOpen, setExportOpen] = useState(false);
  const [bundleOpen, setBundleOpen] = useState(false);
  const [diffTarget, setDiffTarget] = useState<{
    taskId: string;
    fileName: string;
    baseVersion: OfficeVersion;
    version: OfficeVersion;
  }>();
  const [exportName, setExportName] = useState('');
  const [exportedPath, setExportedPath] = useState('');
  const [components, setComponents] = useState<OfficeRendererStatus>();
  const [componentsOpen, setComponentsOpen] = useState(false);
  const operation = useRef(false);
  const alive = useRef(true);
  const currentTaskId = useRef(taskId);
  const uploadAbort = useRef<AbortController | undefined>(undefined);
  const startup = useRef<{ taskId: string; prompt: string } | undefined>(undefined);
  const createdDetail = useRef<OfficeTaskDetail | undefined>(undefined);
  const pendingCreate = useRef<{ goal: string; detail: OfficeTaskDetail; imported: boolean } | undefined>(undefined);
  const initialFileInput = useRef<HTMLInputElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const revisionInput = useRef<HTMLInputElement>(null);
  const revisionTarget = useRef<OfficeImportRevision | undefined>(undefined);
  currentTaskId.current = taskId;
  const deliverables = detail?.artifacts.filter(item => !isOfficeReference(item)) ?? [];
  const artifact = deliverables.find((item) => item.id === artifactId) ?? defaultOfficeArtifact(deliverables);
  const version =
    artifact?.versions.find((item) => item.id === versionId) ??
    artifact?.versions.find((item) => item.id === artifact.headVersionId) ??
    artifact?.versions[0];
  const nodeOffset = previewPage.versionId === version?.id ? previewPage.offset : 0;
  const previousNodeOffsets = previewPage.versionId === version?.id ? previewPage.previous : [];
  const selectedNode =
    selection?.versionId === version?.id && selection?.offset === nodeOffset ? selection.node : undefined;
  const setSelectedNode = (node?: OfficeNode) =>
    setSelection(node && version ? { versionId: version.id, offset: nodeOffset, node } : undefined);

  const applyDetail = useCallback(
    (next: OfficeTaskDetail, selectHead = false) => {
      if (!alive.current || next.task.id !== currentTaskId.current) return;
      setDetail((previous) => visibleDetail(previous, next));
      setTasks((items) => [next.task, ...items.filter((item) => item.id !== next.task.id)]);
      if (selectHead && !next.snapshotIncomplete) {
        const nextArtifact =
          next.artifacts.find((item) => item.id === artifactId) ?? defaultOfficeArtifact(next.artifacts);
        if (nextArtifact) {
          setArtifactId(nextArtifact.id);
          setVersionId(nextArtifact.headVersionId);
          setPreviewPage({ versionId: nextArtifact.headVersionId, offset: 0, previous: [] });
        }
      }
    },
    [artifactId],
  );
  const refreshList = useCallback(async () => {
    setLoading(true);
    try {
      const result = await officeRead(api.list());
      if (alive.current) {
        setTasks(result.items);
        setListError('');
      }
    } catch (cause) {
      if (alive.current) setListError(message(cause));
    } finally {
      if (alive.current) setLoading(false);
    }
  }, [api]);
  useEffect(() => {
    alive.current = true;
    void refreshList();
    return () => {
      alive.current = false;
      uploadAbort.current?.abort();
    };
  }, [refreshList]);
  useEffect(() => {
    let active = true;
    setTaskLoading(!!taskId);
    setTaskLoadError('');
    const seed = createdDetail.current?.task.id === taskId ? createdDetail.current : undefined;
    setDetail(seed);
    createdDetail.current = undefined;
    setArtifactId('');
    setVersionId('');
    setPreview(undefined);
    setSelectedNode(undefined);
    setImageTarget(undefined);
    setNativeTarget(undefined);
    setSheetChartTarget(undefined);
    nativeGeneration.current++;
    setNativeStopRequested(false);
    setChartTarget(undefined);
    setActivity(false);
    setError('');
    try {
      taskId ? localStorage.setItem(LAST_TASK, taskId) : localStorage.removeItem(LAST_TASK);
    } catch {
      /* view state is optional */
    }
    if (taskId)
      void officeRead(api.get({ taskId }))
        .then(async (next) => {
          if (!active) return;
          setDetail((previous) => visibleDetail(previous, next));
          setTaskLoading(false);
          if (next.snapshotIncomplete) return;
          const synced = await officeRead(api.sync({ taskId }));
          if (active) {
            setDetail((previous) => visibleDetail(previous, synced));
            try {
              const focus = JSON.parse(localStorage.getItem(OFFICE_ARTIFACT_FOCUS_KEY) || 'null');
              if (focus?.taskId === taskId && typeof focus.path === 'string') {
                const name = focus.path.split(/[/\\]/).pop();
                const file = synced.artifacts.find(
                  (item) => item.name === name || item.versions.some((candidate) => candidate.path === focus.path),
                );
                if (file) {
                  setArtifactId(file.id);
                  setVersionId(file.headVersionId);
                }
                localStorage.removeItem(OFFICE_ARTIFACT_FOCUS_KEY);
              }
            } catch {
              /* optional view state never blocks task loading */
            }
          }
        })
        .catch((cause) => {
          if (active) {
            if (seed)
              setDetail((previous) => ({
                ...(previous || seed),
                committed: true,
                snapshotIncomplete: true,
                taskSnapshotStale: true,
                loadNotice: SAVED_READ_NOTICE,
              }));
            else setTaskLoadError(message(cause));
          }
        }).finally(() => { if (active) setTaskLoading(false); });
    return () => {
      active = false;
    };
  }, [api, taskId, taskLoadRevision]);
  useEffect(() => {
    const home = () => {
      if (operation.current) { setNotice('当前文件操作尚未结束，请稍后返回任务列表。'); return; }
      startup.current = undefined;
      setTaskId('');
      setFilesOpen(false);
      setTab('conversation');
      setNotice('');
      void refreshList();
    };
    window.addEventListener(OFFICE_STUDIO_HOME_EVENT, home);
    return () => window.removeEventListener(OFFICE_STUDIO_HOME_EVENT, home);
  }, [refreshList]);
  useEffect(() => {
    if (!detail?.task.sessionId) return;
    let wasActive = false;
    const syncActivity = () => {
      const active = listActiveSessionIds().includes(detail.task.sessionId);
      setActivity(active);
      if (wasActive && !active)
        void api[snapshotIncomplete ? 'get' : 'sync']({ taskId })
          .then((next) => applyDetail(next))
          .catch((cause) => {
            if (alive.current && currentTaskId.current === taskId) setError(message(cause));
          });
      wasActive = active;
    };
    syncActivity();
    return subscribeLiveChatRegistry(syncActivity);
  }, [api, taskId, detail?.task.sessionId, snapshotIncomplete, applyDetail]);
  useEffect(() => {
    if (!taskId || !activity) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const sync = async () => {
      try {
        const next = await api[snapshotIncomplete ? 'get' : 'sync']({ taskId });
        if (active) applyDetail(next);
      } catch (cause) {
        if (active) setError(message(cause));
      } finally {
        if (active) timer = setTimeout(() => void sync(), 4000);
      }
    };
    timer = setTimeout(() => void sync(), 1500);
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [api, taskId, activity, snapshotIncomplete, applyDetail]);
  useEffect(() => {
    let active = true;
    setPreview(undefined);
    setPreviewError('');
    setSelectedNode(undefined);
    if (!taskId || !version?.id) {
      setPreviewLoading(false);
      return;
    }
    setPreviewLoading(true);
    void api
      .preview({ taskId, versionId: version.id, ...(nodeOffset ? { nodeOffset } : {}) })
      .then((result) => {
        if (active) setPreview(result);
      })
      .catch((cause) => {
        if (active) setPreviewError(message(cause));
      })
      .finally(() => {
        if (active) setPreviewLoading(false);
      });
    return () => {
      active = false;
    };
  }, [api, taskId, version?.id, previewRevision, nodeOffset]);
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      if (
        event.key === 'Escape' &&
        !renameOpen &&
        !patchOpen &&
        !exportOpen &&
        !bundleOpen &&
        !diffTarget &&
        !componentsOpen && !storageOpen && !imageTarget && !nativeTarget && !chartTarget && !sheetChartTarget
      ) {
        setFilesOpen(false);
        setTab(null);
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [renameOpen, patchOpen, exportOpen, bundleOpen, diffTarget, componentsOpen, storageOpen, imageTarget, nativeTarget, chartTarget, sheetChartTarget]);

  const run = async (action: () => Promise<void>, requiresSnapshot = true) => {
    if (operation.current) return;
    if (requiresSnapshot && snapshotIncomplete) {
      setNotice(SAVED_READ_NOTICE);
      return;
    }
    operation.current = true;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await action();
    } catch (cause) {
      if (alive.current) setError(message(cause));
    } finally {
      operation.current = false;
      if (alive.current) setBusy(false);
    }
  };
  const selectTask = (id: string) => {
    if (operation.current) return;
    if (!id) {
      startup.current = undefined;
      void refreshList();
    }
    setTaskId(id);
    setFilesOpen(false);
    setNotice('');
    setTab('conversation');
  };
  const refreshNative = () => {
    const target = nativeTarget;
    if (!target) return;
    void run(async () => {
      const generation = ++nativeGeneration.current;
      setNativeStopRequested(false);
      try {
        const next = await api.refreshNative({ taskId: target.taskId, artifactId: target.artifactId, versionId: target.versionId, expectedRevision: target.expectedRevision });
        if (!alive.current || currentTaskId.current !== target.taskId || nativeGeneration.current !== generation) return;
        applyDetail(next, true);
        setNativeTarget(undefined);
        setNotice('更新结果已保存为新版本，请核对排版与数据。');
      } finally {
        // A delayed stop acknowledgement must not attach itself to a finished
        // operation or to a subsequently selected task.
        if (nativeGeneration.current === generation) nativeGeneration.current++;
      }
    });
  };
  const stopNative = () => {
    const target = nativeTarget, generation = nativeGeneration.current;
    if (!target || !operation.current || nativeStopRequested) return;
    setNativeStopRequested(true);
    const current = () => alive.current && currentTaskId.current === target.taskId && nativeGeneration.current === generation && operation.current;
    void api.cancel({ taskId: target.taskId }).then(() => {
      if (current()) setNotice('已请求停止，等待当前更新结束。');
    }).catch(cause => {
      if (current()) { setNativeStopRequested(false); setError(message(cause)); }
    });
  };
  const create = () =>
    run(async () => {
      const text = goal.trim();
      if (!text) return;
      let pending = pendingCreate.current;
      if (!pending || pending.goal !== text) {
        const created = await api.create({ title: text.split('\n')[0].slice(0, 80), goal: text });
        pending = { goal: text, detail: created, imported: false };
        pendingCreate.current = pending;
        if (alive.current)
          setTasks((items) => [created.task, ...items.filter((item) => item.id !== created.task.id)]);
      }
      let result = pending.detail;
      if (initialFiles.length && onImportFiles) {
        if (!pending.imported) {
          const controller = new AbortController();
          uploadAbort.current = controller;
          try {
            await onImportFiles(result.task, initialFiles, setNotice, controller.signal);
            pending.imported = true;
          } finally {
            uploadAbort.current = undefined;
          }
        }
        result = await api.get({ taskId: result.task.id });
        pending.detail = result;
      }
      if (!alive.current) return;
      pendingCreate.current = undefined;
      createdDetail.current = result;
      startup.current = { taskId: result.task.id, prompt: text };
      setTasks((items) => [result.task, ...items.filter((item) => item.id !== result.task.id)]);
      setTaskId(result.task.id);
      setGoal('');
      setInitialFiles([]);
      setNotice('');
      setTab('conversation');
    }, false);
  const reload = () => run(async () => {
    applyDetail(await officeRead(api.get({ taskId })));
    setTaskLoadError('');
  }, false);
  const syncNow = () =>
    run(async () => {
      if (detail) {
        const next = await api.sync({ taskId: detail.task.id });
        applyDetail(next);
        setNotice('已同步原对话的交付文件。');
      }
    });
  const accept = (accepted: OfficeVersion) =>
    run(async () => {
      if (!detail || !artifact) return;
      applyDetail(
        await api.accept({
          taskId,
          artifactId: artifact.id,
          versionId: accepted.id,
          expectedRevision: artifact.revision,
        }),
      );
      setNotice(accepted.quality === 'passed' ? '已接受此版本。' : '已接受为草稿，检查状态保持不变。');
    });
  const restore = (restored: OfficeVersion) =>
    run(async () => {
      if (!detail || !artifact) return;
      applyDetail(
        await api.restore({
          taskId,
          artifactId: artifact.id,
          versionId: restored.id,
          expectedRevision: artifact.revision,
        }),
        true,
      );
      setNotice('已从历史内容创建新版本，历史文件仍保留。');
    });
  const validate = () =>
    run(async () => {
      if (version) {
        setChecking(true);
        try {
          applyDetail(await api.validate({ taskId, versionId: version.id }));
          setPreviewRevision((value) => value + 1);
        } finally {
          if (alive.current) setChecking(false);
        }
      }
    });
  const stopCheck = async () => {
    if (stopping) return;
    setStopping(true);
    try {
      applyDetail(await api.cancel({ taskId }));
      setNotice('检查已停止，原文件和已有检查记录仍保留。');
    } catch (cause) {
      setError(message(cause));
    } finally {
      if (alive.current) setStopping(false);
    }
  };
  const importFiles = (files: File[], revision?: OfficeImportRevision) =>
    run(async () => {
      if (!detail || !onImportFiles || !files.length) return;
      const controller = new AbortController();
      uploadAbort.current = controller;
      try {
        await onImportFiles(detail.task, files, setNotice, controller.signal, revision);
        try {
          applyDetail(await api.get({ taskId }), true);
          setNotice(revision ? '已导入为修改草稿，历史和已接受版本保持不变。' : '文件已导入，原文件保持不变。');
        } catch {
          applyDetail({
            ...detail,
            committed: true,
            snapshotIncomplete: true,
            taskSnapshotStale: true,
            loadNotice: SAVED_READ_NOTICE,
          });
        }
      } finally {
        uploadAbort.current = undefined;
      }
    });
  const locate = (node: OfficeNode) => {
    setSelectedNode(node);
    requestAnimationFrame(() =>
      document.getElementById(`office-node-${node.id}`)?.scrollIntoView({ block: 'nearest', behavior: 'auto' }),
    );
  };
  const onChatActivity = (active: boolean) => {
    if (currentTaskId.current !== taskId) return;
    setActivity(active);
    if (!active && taskId)
      void api[snapshotIncomplete ? 'get' : 'sync']({ taskId })
        .then((next) => applyDetail(next))
        .catch((cause) => {
          if (alive.current) setError(message(cause));
        });
  };
  const filter = query.trim().toLocaleLowerCase();
  const visibleTasks = tasks.filter(
    (task) => !filter || `${task.title} ${task.goal}`.toLocaleLowerCase().includes(filter),
  );
  const selectVersion = (id: string) => {
    setVersionId(id);
    setPreviewPage({ versionId: id, offset: 0, previous: [] });
    setSelectedNode(undefined);
  };
  const openExport = () => {
    if (artifact && version) {
      setExportName(`${artifact.name.replace(/\.[^.]+$/, '')}_v${version.versionNo}.${artifact.kind}`);
      setExportedPath('');
      setExportOpen(true);
    }
  };

  return (
    <div
      className={`office-studio ${taskId ? 'is-task' : 'is-home'} ${filesOpen ? 'files-open' : ''} ${tab ? 'inspector-open' : ''}`}
    >
      <header className="os-header">
        <div>
          <button className="os-breadcrumb" onClick={() => selectTask('')}>
            办公工作台
          </button>
          {detail && (
            <>
              <span aria-hidden="true"> / </span>
              <span>{activity ? '执行中' : detail.task.status === 'succeeded' && !defaultOfficeArtifact(detail.artifacts) ? '参考材料已就绪，尚无交付文件' : officeRunLabel(detail.task.status)}</span>
            </>
          )}
          <h1>{detail?.task.title || '从材料到可交付文件'}</h1>
        </div>
        <div className="os-row-actions">
          {taskId && <button className="os-back-to-tasks" disabled={busy} onClick={() => selectTask('')}>
            <ArrowLeft size={16} aria-hidden="true" />{zh ? '返回任务列表' : 'Back to tasks'}
          </button>}
          {detail && (
            <>
              <button disabled={mutationBusy} aria-label="修改任务信息" onClick={() => {
                setTitleDraft(detail.task.title); setGoalDraft(detail.task.goal); setRenameOpen(true);
              }}>修改任务</button>
              <button
                className="os-files-toggle"
                aria-expanded={filesOpen}
                onClick={() => setFilesOpen((value) => !value)}
              >
                任务与文件
              </button>
              <button
                disabled={busy}
                onClick={() =>
                  void run(async () => {
                    await onOpenSession(detail.task);
                  }, false)
                }
              >
                返回原对话
              </button>
              {artifact && version && (artifact.kind === 'docx' || artifact.kind === 'xlsx') && <button disabled={mutationBusy} onClick={() => {setError('');setNativeStopRequested(false);setNativeTarget({taskId,artifactId:artifact.id,versionId:version.id,expectedRevision:artifact.revision,versionNo:version.versionNo,kind:artifact.kind as 'docx'|'xlsx'});}}>{artifact.kind === 'docx' ? '更新目录与页码' : '重新计算公式'}</button>}
              <button disabled={mutationBusy || !version} onClick={openExport}>
                导出副本
              </button>
              <button disabled={mutationBusy || !detail.artifacts.length} onClick={() => setBundleOpen(true)}>
                成套导出
              </button>
            </>
          )}
          <button disabled={busy} onClick={() => setStorageOpen(true)}>存储用量</button>
          {detail && (
            <button
              className="os-primary"
              disabled={busy}
              onClick={() => {
                pendingCreate.current = undefined;
                setGoal('');
                setInitialFiles([]);
                selectTask('');
              }}
            >
              ＋ 新建任务
            </button>
          )}
        </div>
      </header>
      <Dialog open={!!nativeTarget} title={nativeTarget?.kind === 'docx' ? '更新目录与页码' : '重新计算公式'} onClose={() => {if (!busy) setNativeTarget(undefined);}}>
        <div className="os-storage-dialog"><p>根据当前查看的 v{nativeTarget?.versionNo}，使用本机 LibreOffice {nativeTarget?.kind === 'docx' ? '更新文档目录与页码' : '重新计算表格公式缓存'}，另存为新版本。原版本保留；本机缺少组件时会显示具体原因。</p><p className="os-muted">更新完成仍需查看检查结果，核对排版和数据。</p>{error&&<p role="alert">{error}</p>}{busy&&notice&&<p role="status">{notice}</p>}<div className="dialog-actions"><button disabled={busy} onClick={() => setNativeTarget(undefined)}>取消</button>{busy && nativeTarget && <button disabled={nativeStopRequested} onClick={stopNative}>{nativeStopRequested?'已请求停止':'停止更新'}</button>}<button className="primary" disabled={mutationBusy || !nativeTarget} onClick={refreshNative}>{busy ? '正在更新…' : '更新并保存新版本'}</button></div></div>
      </Dialog>
      <OfficeChartDialog api={api} target={chartTarget} onChanged={next => {applyDetail(next, true);setSelectedNode(undefined);setNotice('图表修改已保存为新草稿版本。');}} onClose={() => setChartTarget(undefined)}/>
      <OfficeSheetChartDialog api={api} target={sheetChartTarget} onChanged={next => {applyDetail(next, true);setSelectedNode(undefined);setNotice('图表源范围已保存为新草稿版本。');}} onClose={() => setSheetChartTarget(undefined)}/>
      <OfficeStorageDialog api={api} open={storageOpen} onClose={() => setStorageOpen(false)}/>
      {onUploadImage && <OfficeImageDialog api={api} target={imageTarget} onUpload={onUploadImage} onChanged={next => {applyDetail(next, true);setSelectedNode(undefined);setNotice('已保存图片替换，生成新的草稿版本。');}} onClose={() => setImageTarget(undefined)}/>}
      {error && !nativeTarget && (
        <div className="os-alert" role="alert">
          {error}
          <button onClick={() => void (taskId ? reload() : refreshList())} disabled={busy}>
            重试
          </button>
        </div>
      )}
      {taskLoadError && detail && <div className="os-alert" role="alert">{taskLoadError}<button disabled={busy} onClick={() => void reload()}>{zh ? '重新读取' : 'Reload'}</button></div>}
      {notice && (
        <div className="os-notice" role="status">
          {notice}
          {uploadAbort.current && <button onClick={() => uploadAbort.current?.abort()}>取消上传</button>}
        </div>
      )}
      {detail?.loadNotice && (
        <div className="os-notice" role="status">
          {detail.loadNotice}
          <button disabled={busy} onClick={() => void reload()}>
            重新读取完整记录
          </button>
        </div>
      )}
      {!taskId ? (
        <div className="os-home-scroll">
          <section className="os-home-intro">
            <span className="os-eyebrow">OFFICE STUDIO</span>
            <h2>把内容、数据和表达，放在同一张工作台。</h2>
            <p>告诉月汐你的目标。沿用已有对话和工具，生成真实文件，保留每次修改。</p>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void create();
              }}
            >
              <label htmlFor="office-home-goal" className="os-sr-only">
                想完成什么工作
              </label>
              <textarea
                id="office-home-goal"
                value={goal}
                onChange={(event) => setGoal(event.target.value)}
                placeholder="例如：根据销售数据做一份季度经营汇报，包含图表和行动建议"
                maxLength={2048}
                rows={3}
              />
              {onImportFiles && (
                <>
                  <input
                    ref={initialFileInput}
                    type="file"
                    accept=".pptx,.docx,.xlsx,.pdf"
                    className="os-sr-only"
                    multiple
                    aria-label="添加参考文件"
                    onChange={(event) => {
                      const selected = Array.from(event.target.files || []);
                      event.target.value = '';
                      try {
                        const combined = [...initialFiles, ...selected];
                        validateOfficeFiles(combined);
                        setInitialFiles(combined);
                        setError('');
                      } catch (cause) {
                        setError(message(cause));
                      }
                    }}
                  />
                  {initialFiles.length > 0 && (
                    <div className="os-home-file-list" aria-label="已选参考文件">
                      {initialFiles.map((file, index) => (
                        <span key={`${file.name}:${file.size}:${file.lastModified}:${index}`}>
                          <b>{file.name}</b>
                          <button
                            type="button"
                            aria-label={`移除参考文件 ${file.name}`}
                            onClick={() => setInitialFiles((items) => items.filter((_, itemIndex) => itemIndex !== index))}
                          >
                            ×
                          </button>
                        </span>
                      ))}
                    </div>
                  )}
                </>
              )}
              <div className="os-home-submit-row">
                <div>
                  <span>PPT · Word · Excel · PDF</span>
                  {onImportFiles && (
                    <button type="button" className="os-reference-button" disabled={busy} onClick={() => initialFileInput.current?.click()}>
                      ＋ 参考文件{initialFiles.length ? ` (${initialFiles.length})` : ''}
                    </button>
                  )}
                </div>
                <button className="os-primary" disabled={busy || !goal.trim()}>
                  {busy ? (initialFiles.length ? '正在导入…' : '正在创建…') : '开始工作 →'}
                </button>
              </div>
            </form>
          </section>
          <section className="os-recent">
            <div className="os-section-heading">
              <h2>最近任务</h2>
              <label>
                <span className="os-sr-only">搜索办公任务</span>
                <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索任务" />
              </label>
              <button disabled={loading} onClick={() => void refreshList()}>
                刷新
              </button>
            </div>
            {listError && <p className="os-alert" role="alert">{listError}</p>}
            {loading ? (
              <p role="status">正在读取任务…</p>
            ) : visibleTasks.length ? (
              <div className="os-task-grid">
                {visibleTasks.map((task) => (
                  <button key={task.id} className="os-task-card" onClick={() => selectTask(task.id)}>
                    <span className="os-file-mark" aria-hidden="true">
                      ▤
                    </span>
                    <h3>{task.title}</h3>
                    <p>{task.goal || '继续这个办公任务'}</p>
                    <div>
                      <span>{officeRunLabel(task.status)}</span>
                      <time>{officeDate(task.updatedAt)}</time>
                    </div>
                  </button>
                ))}
              </div>
            ) : (
              <p className="os-muted">{query ? '没有匹配的任务。' : '还没有办公任务。从上面输入目标即可开始。'}</p>
            )}
          </section>
          <button
            className="os-component-entry"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                setComponents(await api.probe());
                setComponentsOpen(true);
              })
            }
          >
            查看本机排版与检查组件
          </button>
        </div>
      ) : (
        <div className="os-workspace" ref={panelResize.workspaceRef} style={{ '--os-inspector-width': `${panelResize.width}px` } as React.CSSProperties}>
          <aside className="os-file-rail" aria-label="任务与文件">
            <div className="os-section-heading">
              <h3>交付文件</h3>
              <button disabled={mutationBusy || !detail} onClick={() => void syncNow()} title="同步原对话中的文件">
                ↻
              </button>
            </div>
            <div className="os-file-list">
              {deliverables.length ? (
                deliverables.map((file) => (
                  <button
                    key={file.id}
                    aria-current={artifact?.id === file.id ? 'true' : undefined}
                    onClick={() => {
                      setArtifactId(file.id);
                      setVersionId(file.headVersionId);
                      setPreviewPage({ versionId: file.headVersionId, offset: 0, previous: [] });
                      setFilesOpen(false);
                    }}
                  >
                    <span className={`os-format is-${file.kind}`}>{file.kind.toUpperCase()}</span>
                    <b>{file.name}</b>
                    <small>
                      {isOfficeReference(file) ? '参考材料' : '交付文件'} · {file.versions.length} 个版本{file.acceptedVersionId ? ' · 已接受' : ''}
                    </small>
                  </button>
                ))
              ) : (
                <p className="os-muted">还没有交付文件</p>
              )}
            </div>
            {onImportFiles && (
              <>
                <input
                  ref={fileInput}
                  type="file"
                  accept=".pptx,.docx,.xlsx,.pdf"
                  className="os-sr-only"
                  multiple
                  onChange={(event) => {
                    const files = Array.from(event.target.files || []);
                    event.target.value = '';
                    void importFiles(files);
                  }}
                />
                <input
                  ref={revisionInput}
                  type="file"
                  accept={artifact ? `.${artifact.kind}` : '.pptx,.docx,.xlsx,.pdf'}
                  className="os-sr-only"
                  aria-label="导入当前文件的修改版"
                  onChange={(event) => {
                    const files = Array.from(event.target.files || []);
                    event.target.value = '';
                    const target = revisionTarget.current;
                    revisionTarget.current = undefined;
                    if (target) void importFiles(files, target);
                  }}
                />
                <button
                  className="os-import"
                  disabled={mutationBusy || !artifact || !version}
                  onClick={() => {
                    if (!artifact || !version) return;
                    revisionTarget.current = {
                      artifactId: artifact.id,
                      baseVersionId: version.id,
                      expectedRevision: artifact.revision,
                      kind: artifact.kind,
                    };
                    revisionInput.current?.click();
                  }}
                >
                  导入当前文件的修改版
                </button>
                <small className="os-muted">单个文件不超过 10 MiB</small>
              </>
            )}
            <details className="os-task-switcher">
              <summary>其他任务</summary>
              {tasks
                .filter((task) => task.id !== taskId)
                .map((task) => (
                  <button key={task.id} disabled={busy} onClick={() => selectTask(task.id)}>
                    {task.title}
                  </button>
                ))}
            </details>
          </aside>
          <div className="os-main">
            <div className="os-document-bar">
              <div>
                <strong>{artifact?.name || '交付文件'}</strong>
                {version && (
                  <span>
                    {artifact && isOfficeReference(artifact) ? '参考材料 · ' : ''}
                    v{version.versionNo}
                    {artifact?.acceptedVersionId === version.id ? ' · 已接受' : ''}
                  </span>
                )}
              </div>
              <nav aria-label="工作台详情">
                {(Object.keys(officeInspectorLabels) as OfficeInspectorTab[]).map((value) => (
                  <button
                    key={value}
                    aria-pressed={tab === value}
                    onClick={() => setTab((current) => (current === value ? null : value))}
                  >
                    {officeInspectorLabels[value]}
                  </button>
                ))}
              </nav>
            </div>
            <OfficeArtifactViewer
              api={api}
              taskId={taskId}
              artifact={artifact}
              version={version}
              preview={preview}
              loading={previewLoading}
              error={previewError}
              selectedNodeId={selectedNode?.id}
              onSelectNode={locate}
              onRetry={() => setPreviewRevision((value) => value + 1)}
            />
            {version && preview && typeof preview.totalNodes === 'number' && preview.totalNodes > 0 && (
              <nav className="os-preview-pages" aria-label="结构内容翻页">
                <button
                  disabled={previewLoading || !previousNodeOffsets.length}
                  onClick={() => {
                    setPreviewPage({
                      versionId: version.id,
                      offset: previousNodeOffsets[previousNodeOffsets.length - 1],
                      previous: previousNodeOffsets.slice(0, -1),
                    });
                  }}
                >
                  上一批内容
                </button>
                <span>
                  {nodeOffset + 1}–{preview.nextNodeOffset ?? nodeOffset + preview.nodes.length} / {preview.totalNodes}{' '}
                  段内容
                </span>
                <button
                  disabled={
                    previewLoading ||
                    !preview.nextNodeOffset ||
                    preview.nextNodeOffset <= nodeOffset ||
                    preview.nextNodeOffset >= preview.totalNodes
                  }
                  onClick={() => {
                    if (preview.nextNodeOffset && preview.nextNodeOffset > nodeOffset)
                      setPreviewPage({
                        versionId: version.id,
                        offset: preview.nextNodeOffset,
                        previous: [...previousNodeOffsets, nodeOffset],
                      });
                  }}
                >
                  下一批内容
                </button>
              </nav>
            )}
            {selectedNode && (
              <div className="os-selection">
                <span>
                  已选择：{selectedNode.label} · v{version?.versionNo}
                </span>
                {selectedNode.editable && selectedNode.valueType !== 'chart' && selectedNode.valueType !== 'image' && selectedNode.digest && (
                  <button
                    disabled={mutationBusy}
                    onClick={() => {
                      if (!artifact || !version || !selectedNode.digest) return;
                      setPatchTarget({
                        taskId,
                        artifactId: artifact.id,
                        baseVersionId: version.id,
                        expectedRevision: artifact.revision,
                        nodeId: selectedNode.id,
                        nodeDigest: selectedNode.digest,
                        label: selectedNode.label,
                        versionNo: version.versionNo,
                        originalText: selectedNode.text,
                      });
                      setPatchText(selectedNode.text);
                      setPatchOpen(true);
                    }}
                  >
                    修改这部分
                  </button>
                )}
                {onUploadImage && artifact?.kind === 'pptx' && version && detail && selectedNode.valueType === 'image' && selectedNode.image && selectedNode.digest && <button disabled={mutationBusy} onClick={() => setImageTarget({task: detail.task, artifactId: artifact.id, baseVersionId: version.id, expectedRevision: artifact.revision, node: selectedNode, versionNo: version.versionNo})}>替换这张图片</button>}
                {artifact?.kind === 'pptx' && version && selectedNode.valueType === 'chart' && selectedNode.chart && selectedNode.editable && selectedNode.digest && <button disabled={mutationBusy} onClick={() => setChartTarget({taskId,artifactId:artifact.id,baseVersionId:version.id,expectedRevision:artifact.revision,versionNo:version.versionNo,node:selectedNode})}>编辑这个图表</button>}
                {artifact?.kind === 'xlsx' && version && selectedNode.valueType === 'chart' && selectedNode.chart?.sourceRanges?.length && selectedNode.location && <button disabled={mutationBusy} onClick={() => {
                  const digest = preview?.parts?.find(part => part.name === selectedNode.location)?.sha256;
                  if (!digest) { setNotice('当前预览还没有工作表摘要，请先重新打开此版本再改图表源范围。'); return; }
                  setSheetChartTarget({taskId,artifactId:artifact.id,baseVersionId:version.id,expectedRevision:artifact.revision,versionNo:version.versionNo,node:selectedNode,partDigest:digest});
                }}>修改图表源范围</button>}
                <button aria-label="清除选区" onClick={() => setSelectedNode(undefined)}>
                  ×
                </button>
              </div>
            )}
            {detail?.steps.length ? (
              <details className="os-process">
                <summary>
                  {activity ? '正在执行' : '执行记录'} · {detail.steps[detail.steps.length - 1].label}
                </summary>
                <ol>
                  {detail.steps.map((step) => (
                    <li key={step.id}>
                      <b>{step.label}</b>
                      <span>{step.status}</span>
                      {step.summary && <p>{step.summary}</p>}
                      {step.summaryTruncated && <small className="os-muted">此处显示执行说明摘要。</small>}
                    </li>
                  ))}
                </ol>
              </details>
            ) : null}
          </div>
          <div className="os-panel-resizer" role="separator" tabIndex={0} hidden={!tab}
            aria-label="调整产物与对话区域宽度" aria-orientation="vertical"
            aria-valuemin={panelResize.min} aria-valuemax={panelResize.max} aria-valuenow={panelResize.width}
            aria-controls="office-inspector" title="调整区域宽度；双击恢复默认"
            onPointerDown={panelResize.start} onKeyDown={panelResize.onKeyDown} onDoubleClick={panelResize.reset} />
          <aside id="office-inspector" className="os-inspector" aria-label="工作台详情面板" hidden={!tab}>
            <div className="os-inspector-heading">
              <h2>{tab ? officeInspectorLabels[tab] : ''}</h2>
              <button aria-label="关闭详情面板" onClick={() => setTab(null)}>
                ×
              </button>
            </div>
            <nav className="os-inspector-tabs" aria-label="详情分类">
              {(Object.keys(officeInspectorLabels) as OfficeInspectorTab[]).map((value) => (
                <button
                  key={value}
                  aria-label={`切换到${officeInspectorLabels[value]}`}
                  aria-pressed={tab === value}
                  onClick={() => setTab(value)}
                >
                  {officeInspectorLabels[value]}
                </button>
              ))}
            </nav>
            <div className="os-conversation" hidden={tab !== 'conversation'}>
              {detail && <OfficeReferences key={`references:${detail.task.id}`} api={api} task={detail.task}
                files={detail.artifacts.filter(isOfficeReference)} onOpenExport={onOpenExport}
                onAdd={onImportFiles && !mutationBusy ? () => fileInput.current?.click() : undefined}/>} 
              {detail && (!snapshotIncomplete || !startup.current) ? (
                renderConversation(detail.task, {
                  initialPrompt: startup.current?.taskId === detail.task.id ? startup.current.prompt : undefined,
                  onReady: () => {
                    if (startup.current?.taskId === detail.task.id) startup.current = undefined;
                  },
                  onActivityChange: onChatActivity,
                })
              ) : taskLoading ? (
                <p role="status">正在读取任务对话…</p>
              ) : (
                <div className="os-load-recovery" role="alert">
                  <p>{taskLoadError || (snapshotIncomplete ? '任务记录尚未完整读取，暂不能开始对话。' : '任务对话暂不可用。')}</p>
                  <div className="os-row-actions">
                    <button onClick={() => setTaskLoadRevision(value => value + 1)}><RefreshCw size={16} aria-hidden="true" />{zh ? '重新读取对话' : 'Retry loading chat'}</button>
                    <button onClick={() => selectTask('')}><ArrowLeft size={16} aria-hidden="true" />{zh ? '返回任务列表' : 'Back to tasks'}</button>
                  </div>
                </div>
              )}
            </div>
            <div className="os-inspector-scroll" hidden={tab === 'conversation' || !tab}>
              {tab === 'checks' && (
                <OfficeChecks
                  version={version}
                  busy={mutationBusy}
                  checking={checking}
                  stopping={stopping}
                  onStop={() => void stopCheck()}
                  onValidate={() => void validate()}
                  onLocate={(id) => {
                    const node = preview?.nodes.find((item) => item.id === id);
                    if (node) locate(node);
                  }}
                />
              )}
              {tab === 'versions' && (
                <OfficeVersions
                  artifact={artifact}
                  selectedVersionId={version?.id}
                  busy={mutationBusy}
                  onSelect={selectVersion}
                  onAccept={(item) => void accept(item)}
                  onRestore={(item) => void restore(item)}
                  onCompare={(item) => {
                    if (artifact && version)
                      setDiffTarget({ taskId, fileName: artifact.name, baseVersion: item, version });
                  }}
                />
              )}
              <div hidden={tab !== 'sources'}>
                {detail && (
                  <OfficeMetricPanel
                    key={taskId}
                    taskId={taskId}
                    readOnly={snapshotIncomplete}
                    artifact={artifact}
                    version={version}
                    node={selectedNode}
                    actions={{
                      list: () => api.listMetrics({ taskId }),
                      capture: (input) => api.captureMetric({ taskId, ...input }),
                      apply: (input) => api.applyMetric({ taskId, ...input }),
                    }}
                    onChanged={applyDetail}
                  />
                )}
                <OfficeSources sources={detail?.sources || []} />
              </div>
            </div>
          </aside>
        </div>
      )}
      <Dialog
        open={renameOpen}
        title="任务信息"
        onClose={() => {
          if (!busy) setRenameOpen(false);
        }}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
              if (detail) {
                applyDetail(
                  await api.update({
                    taskId,
                    expectedRevision: detail.task.revision,
                    title: titleDraft.trim(),
                    goal: goalDraft.trim(),
                  }),
                );
                setRenameOpen(false);
              }
            });
          }}
        >
          <label>
            任务名称
            <input value={titleDraft} onChange={(event) => setTitleDraft(event.target.value)} maxLength={200} />
          </label>
          <label>
            工作目标
            <textarea
              value={goalDraft}
              onChange={(event) => setGoalDraft(event.target.value)}
              maxLength={2048}
              rows={4}
            />
          </label>
          <div className="dialog-actions">
            <button type="button" disabled={busy} onClick={() => setRenameOpen(false)}>
              取消
            </button>
            <button className="primary" disabled={mutationBusy || !titleDraft.trim()}>
              保存
            </button>
          </div>
        </form>
      </Dialog>
      <Dialog
        open={patchOpen}
        title={`修改「${patchTarget?.label || ''}」`}
        description={`以 v${patchTarget?.versionNo} 为基础创建新版本。如果文件已更新，此次修改将提示冲突。`}
        onClose={() => {
          if (!busy) setPatchOpen(false);
        }}
        wide
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
              if (!patchTarget || patchTarget.taskId !== taskId) return;
              applyDetail(
                await api.patch({
                  taskId,
                  artifactId: patchTarget.artifactId,
                  baseVersionId: patchTarget.baseVersionId,
                  expectedRevision: patchTarget.expectedRevision,
                  nodeId: patchTarget.nodeId,
                  nodeDigest: patchTarget.nodeDigest,
                  text: patchText,
                }),
                true,
              );
              setPatchOpen(false);
              setNotice('已生成局部修改版本，请检查受影响内容。');
            });
          }}
        >
          <label>
            修改后的文字
            <textarea
              value={patchText}
              onChange={(event) => setPatchText(event.target.value)}
              maxLength={32000}
              rows={10}
            />
          </label>
          <div className="dialog-actions">
            <button type="button" disabled={busy} onClick={() => setPatchOpen(false)}>
              取消
            </button>
            <button
              className="primary"
              disabled={
                mutationBusy || !patchTarget || patchTarget.taskId !== taskId || patchText === patchTarget.originalText
              }
            >
              保存为新版本
            </button>
          </div>
        </form>
      </Dialog>
      <Dialog
        open={exportOpen}
        title="导出副本"
        description={
          version?.quality === 'passed'
            ? '导出当前查看的版本，历史和已接受版本保持不变。'
            : '此版本尚未通过全部所需检查，将按草稿导出。'
        }
        onClose={() => {
          if (!busy) setExportOpen(false);
        }}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
              if (!version) return;
              const result = await api.exportArtifact({
                taskId,
                versionId: version.id,
                name: exportName.trim(),
                draft: version.quality !== 'passed',
              });
              setExportedPath(result.path);
              setNotice(result.notice || '副本已保存。');
            });
          }}
        >
          <label>
            文件名称
            <input value={exportName} onChange={(event) => setExportName(event.target.value)} maxLength={200} />
          </label>
          {exportedPath && (
            <div className="os-export-result">
              <p>已保存：{exportedPath}</p>
              {onOpenExport && detail && (
                <div className="os-row-actions">
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void run(() => onOpenExport(detail.task, exportedPath, false))}
                  >
                    本机打开
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void run(() => onOpenExport(detail.task, exportedPath, true))}
                  >
                    所在文件夹
                  </button>
                </div>
              )}
            </div>
          )}
          <div className="dialog-actions">
            <button type="button" disabled={busy} onClick={() => setExportOpen(false)}>
              关闭
            </button>
            <button className="primary" disabled={mutationBusy || !exportName.trim()}>
              {busy ? '导出中…' : '保存副本'}
            </button>
          </div>
        </form>
      </Dialog>
      {detail && (
        <OfficeBundleDialog
          key={taskId}
          open={bundleOpen}
          readOnly={snapshotIncomplete}
          taskId={taskId}
          taskTitle={detail.task.title}
          artifacts={detail.artifacts}
          actions={{
            list: () => api.listBundles({ taskId }),
            create: (input) => api.createBundle({ taskId, ...input }),
            export: (input) => api.exportBundle({ taskId, ...input }),
            open: async (path) => {
              await api.openExport({ taskId, path, reveal: true });
            },
          }}
          onClose={() => setBundleOpen(false)}
        />
      )}
      {diffTarget && (
        <OfficeDiffDialog
          key={`${diffTarget.taskId}:${diffTarget.baseVersion.id}:${diffTarget.version.id}`}
          {...diffTarget}
          request={api.diff}
          onClose={() => setDiffTarget(undefined)}
        />
      )}
      <Dialog open={componentsOpen} title="本机排版与检查组件" onClose={() => setComponentsOpen(false)}>
        {components?.components.map((component) => (
          <div className="os-component" key={component.id}>
            <h3>
              {component.label} ·{' '}
              {component.status === 'ready' ? '可用' : component.status === 'unavailable' ? '尚未就绪' : '检查失败'}
            </h3>
            <p>{component.detail}</p>
          </div>
        ))}
        <div className="dialog-actions">
          <button onClick={() => setComponentsOpen(false)}>关闭</button>
        </div>
      </Dialog>
    </div>
  );
}

export default OfficeStudioPage;
