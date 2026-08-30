import { expect, test } from 'vitest'
import { PLUGIN_MARKET, pluginHonestyLabel, pluginOriginLabel } from './pluginMarket'

test('market cards are roster toggles, not installable Git/Python packages', () => {
  expect(PLUGIN_MARKET.every(item => item.honesty === 'builtin-toggle')).toBe(true)
  expect(pluginHonestyLabel('git')).toBe('已是内置工具')
  expect(pluginOriginLabel('local')).toBe('内置开关')
  const git = PLUGIN_MARKET.find(item => item.id === 'git')
  const python = PLUGIN_MARKET.find(item => item.id === 'tool-python')
  const clipboard = PLUGIN_MARKET.find(item => item.id === 'clipboard')
  expect(git?.description).toMatch(/command\.run/)
  expect(git?.description).toMatch(/不会安装 Git/)
  expect(python?.description).toMatch(/不会新装解释器/)
  expect(clipboard?.description).toMatch(/不会新装剪贴板/)
})
