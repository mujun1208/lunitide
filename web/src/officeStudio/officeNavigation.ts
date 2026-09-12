export const OFFICE_STUDIO_OPEN_EVENT = 'lunitide:office-studio-open';
export const OFFICE_LAST_TASK_KEY = 'lunitide:office-studio:last-task';
export const OFFICE_ARTIFACT_FOCUS_KEY = 'lunitide:office-studio:artifact-focus';
export const OFFICE_ARTIFACT_FOCUS_EVENT = 'lunitide:office-artifact-focus';
export const OFFICE_STUDIO_HOME_EVENT = 'lunitide:office-studio-home';

export interface OfficeArtifactFocus {
  taskId: string;
  path: string;
}

export function focusOfficeArtifact(taskId: string, path: string): void {
  window.dispatchEvent(new CustomEvent<OfficeArtifactFocus>(OFFICE_ARTIFACT_FOCUS_EVENT, { detail: { taskId, path } }));
}

export function requestedOfficeTask(): string {
  try {
    const focus = JSON.parse(localStorage.getItem(OFFICE_ARTIFACT_FOCUS_KEY) || 'null');
    return typeof focus?.taskId === 'string' ? focus.taskId : '';
  } catch { return ''; }
}

export function openOfficeHome(): void {
  try { localStorage.removeItem(OFFICE_ARTIFACT_FOCUS_KEY); } catch { /* Optional navigation state. */ }
  window.dispatchEvent(new Event(OFFICE_STUDIO_HOME_EVENT));
}
export interface OfficeOpenRequest {
  sessionId: string;
  path: string;
  finish: (error?: string) => void;
  cancelled: () => boolean;
}

// A small navigation signal keeps the Office UI, styles and bridge facade out
// of the ordinary conversation's eager imports. The App owns the actual route.
export function requestOfficeStudio(sessionId: string, path: string): Promise<void> {
  return new Promise((resolve, reject) => {
    let settled = false;
    const timer = window.setTimeout(() => {
      settled = true;
      reject(new Error('打开办公工作台超时，原对话仍可继续。'));
    }, 35_000);
    const finish = (error?: string) => {
      if (settled) return;
      settled = true;
      window.clearTimeout(timer);
      if (error) reject(new Error(error));
      else resolve();
    };
    window.dispatchEvent(
      new CustomEvent<OfficeOpenRequest>(OFFICE_STUDIO_OPEN_EVENT, {
        detail: { sessionId, path, finish, cancelled: () => settled },
      }),
    );
  });
}
