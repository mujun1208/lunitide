import { afterEach, expect, it, vi } from 'vitest'
import { agentHubApi, setAgentHubRequest } from './agentHubApi'

afterEach(() => {
  setAgentHubRequest(undefined)
})

it('sends thread and workspace methods and accepts threadId on file preview', async () => {
  const request = vi.fn().mockResolvedValue({ ok: true })
  setAgentHubRequest(request)
  await agentHubApi.threadCreate({ harnessId: 'cursor', scene: 'write_project', workspaceRoot: 'C:/proj' })
  await agentHubApi.threadGet({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE' })
  await agentHubApi.threadList({})
  await agentHubApi.threadUpdate({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', title: '置顶会话', pinned: true })
  await agentHubApi.threadDelete({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE' })
  await agentHubApi.threadCancel({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE' })
  await agentHubApi.threadPrompt({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', text: '选哪个?' })
  await agentHubApi.threadRespond({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', callId: '01ARZ3NDEKTSV4RRFFQ69G5FAF', optionId: '是' })
  await agentHubApi.workspaceList({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE' })
  await agentHubApi.preview({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', path: 'loopback.txt' })
  await agentHubApi.open({ threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', path: 'loopback.txt' })
  expect(request.mock.calls.map(call => call[0])).toEqual([
    'agentHub.thread.create',
    'agentHub.thread.get',
    'agentHub.thread.list',
    'agentHub.thread.update',
    'agentHub.thread.delete',
    'agentHub.thread.cancel',
    'agentHub.thread.prompt',
    'agentHub.thread.respond',
    'agentHub.workspace.list',
    'agentHub.file.preview',
    'agentHub.file.open',
  ])
  expect(request).toHaveBeenCalledWith('agentHub.file.preview', { threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAE', path: 'loopback.txt' })
})
