import type { AttachmentBridge, LocalWorkspaceBridge } from '../bridge/client'
import { agentHubApi } from './agentHubApi'
import { visibleWorkspaceEntry } from './agentHubCopy'

export const HUB_ATTACHMENTS = {
  list: async () => ({ items: [] }),
  get: async () => {
    throw new Error('没有附件')
  },
  ingest: async () => {
    throw new Error('没有附件')
  },
  delete: async () => {
    throw new Error('没有附件')
  },
  begin: async () => {
    throw new Error('没有附件')
  },
  chunk: async () => {
    throw new Error('没有附件')
  },
  commit: async () => {
    throw new Error('没有附件')
  },
  abort: async () => {
    throw new Error('没有附件')
  },
} as AttachmentBridge

export function createAgentHubLocalWorkspace(opts: {
  threadId: () => string
  root: () => string
}): LocalWorkspaceBridge {
  const folderName = () => opts.root().replace(/\\/g, '/').split('/').filter(Boolean).at(-1) ?? opts.root()
  return {
    root: async () => {
      const path = opts.root()
      return { name: folderName(), path, bound: Boolean(path) }
    },
    select: async () => {
      const path = opts.root()
      return { name: folderName(), path }
    },
    clear: async () => ({ cleared: true }),
    list: async (path = '') => {
      const listed = await agentHubApi.workspaceList({
        threadId: opts.threadId(),
        ...(path ? { relativePath: path } : {}),
      })
      return {
        items: (listed.items ?? [])
          .filter(item => visibleWorkspaceEntry(item.name))
          .map(item => ({ name: item.name, path: item.path, directory: item.isDir })),
        truncated: false,
      }
    },
    read: async path => {
      const preview = await agentHubApi.preview({ threadId: opts.threadId(), path })
      return { path: preview.path || path, content: preview.content ?? '', size: preview.size }
    },
    open: async payload => {
      const path = payload?.path ?? ''
      const opened = await agentHubApi.open({ threadId: opts.threadId(), path })
      return { opened: opened.opened || path, editor: payload?.editor ?? 'explorer' }
    },
  }
}
