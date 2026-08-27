import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'
import { ComposerReplyChips } from './ComposerReplyChips'

afterEach(() => {
  cleanup()
  localStorage.removeItem('lunitide:general')
})

describe('ComposerReplyChips', () => {
  test('writes reply style into general settings', () => {
    render(<ComposerReplyChips />)
    fireEvent.click(screen.getByRole('button', { name: '说话风格' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /老师/ }))
    expect(JSON.parse(localStorage.getItem('lunitide:general') || '{}').replyStyle).toBe('teacher')
  })

  test('closes 默认/关闭 menus on outside click without blocking an inside pick', () => {
    render(
      <>
        <ComposerReplyChips />
        <button type="button">elsewhere</button>
      </>,
    )

    fireEvent.click(screen.getByRole('button', { name: '结构化输出' }))
    expect(screen.getByRole('menuitem', { name: /提取事件/ })).toBeInTheDocument()
    fireEvent.pointerDown(screen.getByRole('button', { name: 'elsewhere' }))
    expect(screen.queryByRole('menuitem', { name: /提取事件/ })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: '说话风格' }))
    expect(screen.getByRole('menuitem', { name: /老师/ })).toBeInTheDocument()
    fireEvent.click(document.body)
    expect(screen.queryByRole('menuitem', { name: /老师/ })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: '结构化输出' }))
    fireEvent.click(screen.getByRole('menuitem', { name: /键值摘要/ }))
    expect(screen.queryByRole('menuitem', { name: /键值摘要/ })).toBeNull()
    expect(JSON.parse(localStorage.getItem('lunitide:general') || '{}').structuredTemplate).toBe('kv')
  })
})
