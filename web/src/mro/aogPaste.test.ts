import { expect, it } from 'vitest'
import { parseAogPaste } from './aogPaste'

it('parses labeled AOG lines', () => {
  const got = parseAogPaste('机尾: B-0001\n件号: NAS1149\n数量: 2\nAOG now')
  expect(got).toMatchObject({ tailNo: 'B-0001', pn: 'NAS1149', qty: '2' })
})

it('extracts tail, PN and qty from the workbench free-text placeholder', () => {
  const got = parseAogPaste('B-1234 AOG 需要 PN 3G2000-1 数量2')
  expect(got).toMatchObject({ tailNo: 'B-1234', pn: '3G2000-1', qty: '2' })
})
