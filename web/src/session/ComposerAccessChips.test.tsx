import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'
import { ComposerAccessChips } from './ComposerAccessChips'

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
})
