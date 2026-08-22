import { expect, it } from 'vitest'
import { codeBlockLanguage, isTerminalLanguage, languageLabel } from './RichCodeBlock'

it('detects fenced languages and terminal labels', () => {
  expect(codeBlockLanguage('language-powershell')).toBe('powershell')
  expect(isTerminalLanguage('bash')).toBe(true)
  expect(languageLabel('env')).toBe('env')
  expect(languageLabel('tsx')).toBe('TSX')
})
