import { cleanup, fireEvent, render } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { CompanionAsk } from './CompanionAsk'

afterEach(cleanup)

describe('CompanionAsk', () => {
  test('empty submit is ignored; a typed line goes to onSend and clears', () => {
    const onSend = vi.fn()
    const { container } = render(<CompanionAsk language="zh" onSend={onSend} />)
    const form = container.querySelector('.companion-ask') as HTMLFormElement
    const input = container.querySelector('input') as HTMLInputElement
    const send = container.querySelector('button') as HTMLButtonElement
    expect(send.disabled).toBe(true)
    fireEvent.submit(form)
    expect(onSend).not.toHaveBeenCalled()
    fireEvent.change(input, { target: { value: '  今天天气怎么样？  ' } })
    expect(send.disabled).toBe(false)
    fireEvent.submit(form)
    expect(onSend).toHaveBeenCalledWith('今天天气怎么样？')
    expect(input.value).toBe('')
  })
})
