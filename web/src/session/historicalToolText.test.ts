import {expect,it} from 'vitest'
import {historicalToolText} from './historicalToolText'

it('removes only the engine bookkeeping envelope and retains tool results',()=>{
  expect(historicalToolText('[tool-result callId=call-1 argsDigest=aaa resultDigest=bbb]\n用户已提交决定，请根据选择继续。')).toBe('用户已提交决定，请根据选择继续。')
  expect(historicalToolText('[tool-result callId=call-1]')).toBe('工具执行完毕')
  expect(historicalToolText('正常结果\n[tool-result 示例]')).toBe('正常结果\n[tool-result 示例]')
})
