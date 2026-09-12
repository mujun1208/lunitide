import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { ComposerAccessChips, laneWordFromGuidance } from './ComposerAccessChips'

afterEach(cleanup)

describe('ComposerAccessChips', () => {
  test('shows git readonly and whitelist shell away from persona chips', () => {
    render(<ComposerAccessChips executionMode="auto-edit" />)
    expect(screen.getByLabelText('编码权限')).toBeInTheDocument()
    expect(screen.getByText('Git 只读')).toBeInTheDocument()
    expect(screen.getByText('Shell 白名单命令')).toBeInTheDocument()
  })

  test('full-access switches the shell chip only', () => {
    render(<ComposerAccessChips executionMode="full-access" />)
    expect(screen.getByText('Git 只读')).toBeInTheDocument()
    expect(screen.getByText('Shell 完全访问')).toBeInTheDocument()
  })

  test('shows a read-only lane word and inserts override phrases', async () => {
    const onInsert = vi.fn()
    render(<ComposerAccessChips executionMode="auto-edit" lane="档位:先问缺什么" onInsertPhrase={onInsert} />)
    expect(screen.getByText('档位:先问缺什么')).toBeInTheDocument()
    expect(screen.getByText('档位:先问缺什么').tagName).not.toBe('BUTTON')
    await userEvent.click(screen.getByRole('button', { name: '深度思考' }))
    await userEvent.click(screen.getByRole('button', { name: '先搜索' }))
    expect(onInsert).toHaveBeenCalledWith('深度思考')
    expect(onInsert).toHaveBeenCalledWith('先搜索')
  })

  test('reads the last-turn lane word from guidance labels', () => {
    expect(laneWordFromGuidance('档位:先问缺什么 / 工作流 / 身份')).toBe('档位:先问缺什么')
    expect(laneWordFromGuidance('工作流 / 身份')).toBe('')
  })
})
