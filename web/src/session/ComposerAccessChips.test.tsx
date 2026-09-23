import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'
import { ComposerAccessChips, laneWordFromGuidance } from './ComposerAccessChips'

afterEach(cleanup)

describe('ComposerAccessChips', () => {
  test('does not show git or shell access chips', () => {
    render(<ComposerAccessChips executionMode="full-access" />)
    expect(screen.queryByText('Git 只读')).toBeNull()
    expect(screen.queryByText('Shell 完全访问')).toBeNull()
    expect(screen.queryByText('Shell 白名单命令')).toBeNull()
  })

  test('shows a read-only lane word without phrase shortcuts', () => {
    render(<ComposerAccessChips executionMode="auto-edit" lane="档位:先问缺什么" />)
    expect(screen.getByText('档位:先问缺什么')).toBeInTheDocument()
    expect(screen.getByText('档位:先问缺什么').tagName).not.toBe('BUTTON')
    expect(screen.queryByRole('button', { name: '深度思考' })).toBeNull()
    expect(screen.queryByRole('button', { name: '先搜索' })).toBeNull()
  })

  test('reads the last-turn lane word from guidance labels', () => {
    expect(laneWordFromGuidance('档位:先问缺什么 / 工作流 / 身份')).toBe('档位:先问缺什么')
    expect(laneWordFromGuidance('工作流 / 身份')).toBe('')
  })
})
