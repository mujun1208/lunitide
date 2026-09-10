import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { type DatasourceWriteOperation } from '../bridge/client'
import { DataSourceWriteDialog, type DatasourceWriteAPI } from './DataSourceWriteDialog'

afterEach(cleanup)
const id='01ARZ3NDEKTSV4RRFFQ69G5FAV'
const op:DatasourceWriteOperation={id,connectionId:id,connectionName:'本机测试库',sql:'UPDATE x SET n=2',digest:'a'.repeat(64),state:'prepared',createdAt:'2026-09-06T00:00:00Z',expiresAt:'2026-09-06T00:10:00Z'}
const fixture=(overrides:Partial<DatasourceWriteAPI>={}):DatasourceWriteAPI=>({
  writeList:vi.fn().mockResolvedValue({items:[]}),writeGet:vi.fn().mockResolvedValue(op),
  writePrepare:vi.fn().mockResolvedValue(op),writeCommit:vi.fn().mockResolvedValue({...op,state:'completed'}),...overrides,
})
function show(api:DatasourceWriteAPI){render(<LanguageProvider value="zh-CN"><DataSourceWriteDialog connectionId={id} name="本机测试库" api={api} onClose={()=>{}} /></LanguageProvider>)}

it('does not show raw English history or write failures',async()=>{
  show(fixture({writeList:vi.fn().mockRejectedValue(new Error('Failed to fetch'))}))
  expect(await screen.findByRole('alert')).toHaveTextContent('无法读取操作记录')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  const api=fixture({writePrepare:vi.fn().mockRejectedValue(new Error('Failed to fetch'))})
  show(api)
  fireEvent.change(screen.getByLabelText('SQL 语句'),{target:{value:op.sql}})
  fireEvent.click(screen.getByRole('button',{name:'检查写入'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('操作失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('executes only the reviewed operation after the second explicit action',async()=>{
  const api=fixture();show(api)
  fireEvent.change(screen.getByLabelText('SQL 语句'),{target:{value:op.sql}})
  fireEvent.click(screen.getByRole('button',{name:'检查写入'}))
  await screen.findByRole('button',{name:'确认执行'})
  expect(api.writeCommit).not.toHaveBeenCalled()
  expect(screen.getByText(op.sql)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button',{name:'确认执行'}))
  await screen.findByText('写入已完成')
  expect(api.writeCommit).toHaveBeenCalledExactlyOnceWith({id:op.id,digest:op.digest})
})

it('reuses the prepare attempt after lost ACK and recovers commit outcome by ID',async()=>{
  const prepare=vi.fn().mockRejectedValueOnce(new Error('ACK lost')).mockResolvedValue(op)
  const api=fixture({writePrepare:prepare,writeCommit:vi.fn().mockRejectedValue(new Error('Commit ACK lost')),writeGet:vi.fn().mockResolvedValue({...op,state:'completed'})});show(api)
  fireEvent.change(screen.getByLabelText('SQL 语句'),{target:{value:op.sql}})
  fireEvent.click(screen.getByRole('button',{name:'检查写入'}));await screen.findByText('操作失败')
  fireEvent.click(screen.getByRole('button',{name:'检查写入'}));await screen.findByRole('button',{name:'确认执行'})
  expect(prepare.mock.calls[0][1].attempt.idempotencyKey).toBe(prepare.mock.calls[1][1].attempt.idempotencyKey)
  fireEvent.click(screen.getByRole('button',{name:'确认执行'}));await screen.findByText('操作失败')
  fireEvent.click(screen.getByRole('button',{name:'核查执行结果'}));await screen.findByText('写入已完成')
  expect(api.writeCommit).toHaveBeenCalledTimes(1)
  expect(api.writeGet).toHaveBeenCalledWith({id:op.id})
})

it('restores unresolved historical operations without automatically resubmitting',async()=>{
  const api=fixture({writeList:vi.fn().mockResolvedValue({items:[{...op,state:'unknown'}]}),writeGet:vi.fn().mockResolvedValue({...op,state:'unknown'})});show(api)
  const history=await screen.findByRole('button',{name:/结果未确认/})
  fireEvent.click(history)
  await waitFor(()=>expect(api.writeGet).toHaveBeenCalledWith({id:op.id}))
  expect(api.writeCommit).not.toHaveBeenCalled()
  expect(screen.queryByRole('button',{name:'确认执行'})).not.toBeInTheDocument()
})
