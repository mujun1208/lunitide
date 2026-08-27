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
})
