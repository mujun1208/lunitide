import React from'react'
import{cleanup,fireEvent,render,screen,waitFor}from'@testing-library/react'
import{afterEach,describe,it,expect,vi}from'vitest'
import{McpCredentialDialog}from'./McpCredentialDialog'
import{McpSecurityReviewDialog}from'./McpSecurityReviewDialog'

const endpoint={endpointId:'mcp-fixture',transport:'https' as const,state:'quarantined' as const,enabled:true,securityVersion:3}
afterEach(cleanup)
describe('MCP security dialogs',()=>{
 it('does not show raw English inspect or accept failures',async()=>{
  render(<McpSecurityReviewDialog endpoint={endpoint} review={vi.fn().mockRejectedValue(new Error('Failed to fetch'))} onClose={()=>{}} onSaved={()=>{}}/>)
  expect(await screen.findByRole('alert')).toHaveTextContent('无法读取服务器定义')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('clears password after sending and retries the same operation after lost acknowledgement',async()=>{
  const save=vi.fn().mockRejectedValueOnce(new Error('结果待确认')).mockResolvedValue({configured:true,securityVersion:4}),onClose=vi.fn(),onSaved=vi.fn()
  render(<McpCredentialDialog endpoint={endpoint} save={save} onClose={onClose} onSaved={onSaved}/> )
  fireEvent.change(screen.getByLabelText('MCP 凭据值'),{target:{value:'fixture-token'}});fireEvent.click(screen.getByText('保存凭据'))
  await screen.findByRole('alert');expect(screen.getByLabelText('MCP 凭据值')).toHaveValue('')
  fireEvent.change(screen.getByLabelText('MCP 凭据值'),{target:{value:'fixture-token'}});fireEvent.click(screen.getByText('保存凭据'))
  await waitFor(()=>expect(onSaved).toHaveBeenCalledOnce());expect(save.mock.calls[0][0]).toEqual(save.mock.calls[1][0]);expect(onClose).toHaveBeenCalledOnce()
 })
 it('requires a real version before credential writes',()=>{
  const save=vi.fn();render(<McpCredentialDialog endpoint={{...endpoint,securityVersion:undefined}} save={save} onClose={()=>{}} onSaved={()=>{}}/> )
  fireEvent.change(screen.getByLabelText('MCP 凭据值'),{target:{value:'fixture-token'}});expect(screen.getByText('保存凭据')).toBeDisabled();expect(screen.getByText('撤销凭据')).toBeDisabled();expect(save).not.toHaveBeenCalled()
 })
 it('accepts only the inspected digest after explicit review and retains failure for retry',async()=>{
  const observed={observedDigest:'a'.repeat(64),identityDigest:'b'.repeat(64),tools:['lookup'],lockedArgs:[],securityVersion:3,accepted:false}
  const review=vi.fn().mockResolvedValueOnce(observed).mockRejectedValueOnce(new Error('服务器再次变化')),onSaved=vi.fn()
  render(<McpSecurityReviewDialog endpoint={endpoint} review={review} onClose={()=>{}} onSaved={onSaved}/> )
  await screen.findByText('工具（1）：lookup');expect(screen.getByText('确认变更')).toBeDisabled();fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByText('确认变更'));await screen.findByText('服务器再次变化')
  expect(review.mock.calls[1][0]).toEqual({endpointId:endpoint.endpointId,expectedVersion:3,action:'accept',observedDigest:observed.observedDigest,confirmed:true});expect(onSaved).not.toHaveBeenCalled()
 })
})

it('saves multiple environment credentials using the returned version and distinct operations', async()=>{
 const save=vi.fn().mockResolvedValueOnce({configured:true,securityVersion:4}).mockResolvedValueOnce({configured:true,securityVersion:5}),onClose=vi.fn()
 render(<McpCredentialDialog endpoint={{...endpoint,transport:'stdio'}} suggestedEnvs={['QDRANT_URL','QDRANT_API_KEY']} save={save} onClose={onClose} onSaved={()=>{}}/>)
 expect(screen.getByLabelText('凭据环境变量')).toHaveValue('QDRANT_URL')
 fireEvent.change(screen.getByLabelText('MCP 凭据值'),{target:{value:'https://fixture.invalid'}})
 fireEvent.click(screen.getByText('保存并继续配置'))
 await screen.findByText('已保存 QDRANT_URL，可以继续配置下一项')
 expect(screen.getByLabelText('凭据环境变量')).toHaveValue('QDRANT_API_KEY');expect(onClose).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('MCP 凭据值'),{target:{value:'fixture-secret'}});fireEvent.click(screen.getByText('保存凭据'))
 await waitFor(()=>expect(onClose).toHaveBeenCalledOnce())
 expect(save.mock.calls[0][0]).toMatchObject({env:'QDRANT_URL',expectedVersion:3})
 expect(save.mock.calls[1][0]).toMatchObject({env:'QDRANT_API_KEY',expectedVersion:4})
 expect(save.mock.calls[0][0].requestId).not.toBe(save.mock.calls[1][0].requestId)
})
