import {uploadOfficeImage} from './officeImageUpload';
import React, { useEffect, useRef, useState } from 'react';
import type {
  AttachmentBridge,
  ChatBridge,
  ExpertBridge,
  MessageBridge,
  ProjectBridge,
  ProviderBridge,
  SessionBridge,
} from '../bridge/client';
import { getDesktopFilesBridge } from '../bridge/client';
import type { ProjectDTO, SessionDTO } from '../generated/bridge';
import { findPersonalProject } from '../app/appHelpers';
import type { LaunchTarget } from '../app/appTypes';
import { SessionPage } from '../session/SessionPage';
import { OfficeStudioPage, type OfficeConversationOptions } from './OfficeStudioPage';
import { officeStudioApi, type OfficeStudioApi, type OfficeTask } from './officeStudioApi';
import { uploadOfficeFiles } from './officeUpload';
import { officeRead } from './officeRead';
import { officeStudioUserError } from './officeUserError';

interface OfficeRouteProps {
  initialTaskId?: string;
  projects: ProjectBridge;
  sessions: SessionBridge;
  messages: MessageBridge;
  attachments: AttachmentBridge;
  providers: ProviderBridge;
  experts: ExpertBridge;
  chat: ChatBridge;
  providersRevision: number;
  onOpenChat: (target: LaunchTarget) => void;
  onManageModels: () => void;
  onActivityChange: (sessionId: string, active: boolean) => void;
  api?: OfficeStudioApi;
}
export interface OfficeBinding {
  project: ProjectDTO;
  session: SessionDTO;
  personal: boolean;
}
export function keepOfficeBinding(previous: OfficeBinding | undefined, next: OfficeBinding): OfficeBinding {
  if (
    previous &&
    previous.session.id === next.session.id &&
    previous.project.id === next.project.id &&
    previous.personal === next.personal
  ) {
    return previous;
  }
  return next;
}
export async function resolveOfficeBinding(
  task: OfficeTask,
  projects: ProjectBridge,
  sessions: SessionBridge,
): Promise<OfficeBinding> {
  const listed = await projects.list();
  const project =
    listed.items.find((item) => item.id === task.projectId) ??
    (!task.projectId ? findPersonalProject(listed.items) : undefined);
  if (!project) throw new Error('此办公任务的会话所属项目当前不可用，请刷新或恢复原对话。');
  const session = (await sessions.list({ projectId: project.id })).items.find((item) => item.id === task.sessionId);
  if (!session) throw new Error('原对话当前不可用，文件仍保留在工作台中。');
  return { project, session, personal: findPersonalProject(listed.items)?.id === project.id };
}

function originalTarget(binding: OfficeBinding): LaunchTarget {
  return binding.personal
    ? { project: binding.project, session: binding.session, personal: true, noAutoSend: true }
    : { project: binding.project, session: binding.session, personal: false };
}

function OfficeConversationHost({
  task,
  options,
  route,
}: {
  task: OfficeTask;
  options: OfficeConversationOptions;
  route: OfficeRouteProps;
}): React.JSX.Element {
  const [binding, setBinding] = useState<OfficeBinding>();
  const [error, setError] = useState('');
  const [revision, setRevision] = useState(0);
  // Capture only this mount's initial request. Subsequent task polling cannot
  // change a draft or send the original request for a second time.
  const initialPrompt = useRef(options.initialPrompt);
  const readyRef = useRef(options.onReady);
  const activityRef = useRef(options.onActivityChange);
  activityRef.current = options.onActivityChange;
  useEffect(() => {
    let active = true;
    setError('');
    void officeRead(resolveOfficeBinding(task, route.projects, route.sessions))
      .then((result) => {
        if (active) {
          setBinding((previous) => keepOfficeBinding(previous, result));
          readyRef.current();
        }
      })
      .catch((cause) => {
        if (active) setError(officeStudioUserError(cause, '原对话加载失败。'));
      });
    return () => {
      active = false;
    };
  }, [task.sessionId, task.projectId, route.projects, route.sessions, revision]);
  if (error)
    return (
      <div className="os-alert" role="alert">
        {error}
        <button onClick={() => setRevision((value) => value + 1)}>重试</button>
      </div>
    );
  if (!binding)
    return (
      <p className="os-inline-status" role="status">
        正在连接原对话…
      </p>
    );
  return (
    <div className="os-conversation-host">
      <SessionPage
        key={binding.session.id}
        project={binding.project}
        initialSession={binding.session}
        officeTaskId={task.id}
        bridge={route.sessions}
        messages={route.messages}
        attachments={route.attachments}
        chat={route.chat}
        providers={route.providers}
        experts={route.experts}
        providersRevision={route.providersRevision}
        desktopFiles={getDesktopFilesBridge()}
        initialPrompt={initialPrompt.current}
        initialNoAutoSend={!initialPrompt.current}
        onBack={() => route.onOpenChat(originalTarget(binding))}
        onActivityChange={(active) => {
          route.onActivityChange(binding.session.id, active);
          activityRef.current(active);
        }}
        onManageModels={route.onManageModels}
        personal={binding.personal}
        homeChat
        readOnly={binding.project.status === 'closed' || binding.project.status === 'archived'}
      />
    </div>
  );
}

export function OfficeStudioRoute(props: OfficeRouteProps): React.JSX.Element {
  const api = props.api ?? officeStudioApi;
  return (
    <OfficeStudioPage
      api={api}
      initialTaskId={props.initialTaskId}
      renderConversation={(task, options) => (
        <OfficeConversationHost key={task.id} task={task} options={options} route={props} />
      )}
      onOpenSession={async (task) => {
        const binding = await resolveOfficeBinding(task, props.projects, props.sessions);
        props.onOpenChat(originalTarget(binding));
      }}
      onImportFiles={async (task, files, progress, signal, revision) => {
        const binding = await resolveOfficeBinding(task, props.projects, props.sessions);
        await uploadOfficeFiles(props.attachments, api, task, binding.project.id, files, progress, signal, revision);
      }}
      onUploadImage={async (task, file, progress, signal) => {
        const binding = await resolveOfficeBinding(task, props.projects, props.sessions);
        return uploadOfficeImage(props.attachments, binding.project.id, task.sessionId, file, progress, signal);
      }}
      onOpenExport={(task, path, reveal) => api.openExport({ taskId: task.id, path, reveal }).then(() => {})}
    />
  );
}

export default OfficeStudioRoute;
