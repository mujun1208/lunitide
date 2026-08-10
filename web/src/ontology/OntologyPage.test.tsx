import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { OntologyBridge } from '../bridge/client'
import type { OntologyNodeDTO, OntologyEdgeDTO } from '../generated/bridge'
import { OntologyPage } from './OntologyPage'

afterEach(cleanup)
const P = '01ARZ3NDEKTSV4RRFFQ69G5FAV', now = '2025-01-01T00:00:00Z'
const node: OntologyNodeDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', projectId: P, type: 'function', name: 'doStuff',
  fullPath: 'src/mod/doStuff', description: '做事', metadataJson: '{}', version: 1, createdAt: now, updatedAt: now,
}
const edge: OntologyEdgeDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', sourceNodeId: node.id, targetNodeId: '01ARZ3NDEKTSV4RRFFQ69G5FAD',
  type: 'depends_on', label: '依赖', propertiesJson: '{}', weight: 1, version: 1, createdAt: now, updatedAt: now,
}
const api = (o: Partial<OntologyBridge> = {}): OntologyBridge => ({
  getNode: vi.fn(), listNodes: vi.fn().mockResolvedValue({ items: [] }), searchNodes: vi.fn().mockResolvedValue({ items: [] }),
  createNode: vi.fn().mockResolvedValue(node), updateNode: vi.fn().mockResolvedValue(node), deleteNode: vi.fn().mockResolvedValue({ deleted: true }),
  listEdges: vi.fn().mockResolvedValue({ items: [] }), createEdge: vi.fn().mockResolvedValue(edge),
  updateEdge: vi.fn().mockResolvedValue(edge), deleteEdge: vi.fn().mockResolvedValue({ deleted: true }), ...o,
})

it('renders empty state and loads nodes for the project', async () => {
  const bridge = api()
  render(<OntologyPage projectId={P} bridge={bridge} />)
  expect(await screen.findByText('暂无节点')).toBeInTheDocument()
  expect(bridge.listNodes).toHaveBeenCalledWith({ projectId: P })
})

it('renders nodes from bridge.listNodes', async () => {
  const bridge = api({ listNodes: vi.fn().mockResolvedValue({ items: [node] }) })
  render(<OntologyPage projectId={P} bridge={bridge} />)
  expect(await screen.findByText('doStuff')).toBeInTheDocument()
})

it('creates a node via the create form', async () => {
  const createNode = vi.fn().mockResolvedValue(node), bridge = api({ createNode })
  render(<OntologyPage projectId={P} bridge={bridge} />)
  await screen.findByText('暂无节点')
  fireEvent.click(screen.getByText('新建节点'))
  fireEvent.change(screen.getByLabelText('节点名称'), { target: { value: '新函数' } })
  fireEvent.change(screen.getByLabelText('完整路径'), { target: { value: 'src/new' } })
  fireEvent.change(screen.getByLabelText('节点描述'), { target: { value: '新描述' } })
  fireEvent.click(screen.getByRole('button', { name: '创建节点' }))
  await waitFor(() => expect(createNode).toHaveBeenCalledOnce())
  expect(createNode.mock.calls[0][0]).toMatchObject({ projectId: P, name: '新函数', fullPath: 'src/new', description: '新描述' })
})

it('deletes a node and creates an edge after selecting a node', async () => {
  const deleteNode = vi.fn().mockResolvedValue({ deleted: true }), createEdge = vi.fn().mockResolvedValue(edge)
  const bridge = api({ listNodes: vi.fn().mockResolvedValue({ items: [node] }), listEdges: vi.fn().mockResolvedValue({ items: [edge] }), deleteNode, createEdge })
  render(<OntologyPage projectId={P} bridge={bridge} />)
  await screen.findByText('doStuff')
  fireEvent.click(screen.getByText('doStuff'))
  await screen.findByText(/01ARZ3NDEKTSV4RRFFQ69G5FAD/)
  fireEvent.click(screen.getByText('新建边'))
  fireEvent.change(screen.getByLabelText('目标节点 ID'), { target: { value: '01ARZ3NDEKTSV4RRFFQ69G5FAD' } })
  fireEvent.change(screen.getByLabelText('边标签'), { target: { value: '新依赖' } })
  fireEvent.click(screen.getByRole('button', { name: '创建边' }))
  await waitFor(() => expect(createEdge).toHaveBeenCalledOnce())
  expect(createEdge.mock.calls[0][0]).toMatchObject({ sourceNodeId: node.id, targetNodeId: '01ARZ3NDEKTSV4RRFFQ69G5FAD', label: '新依赖' })
})
