import type { LocalWorkspaceBridge } from '../bridge/client'
import type { WorkspaceTab } from '../workspace/Workspace'

export type WorkbenchNav = {
  openWorkspace: (tab?: WorkspaceTab) => void
  openApproval: () => void
}

export type WorkbenchStats = {
  running: boolean
  changes: number
}

export async function openWorkspaceInEditor(
  bridge: LocalWorkspaceBridge,
  relativePath?: string,
): Promise<void> {
  await bridge.open({ path: relativePath?.trim() || undefined, editor: 'vscode' })
}
