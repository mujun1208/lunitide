import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { CodeWorkbench } from './CodeWorkbench'

describe('code workbench', () => {
  it('shows the diagnostic, the suggestion, and writes the accepted line', async () => {
    const accept = vi.fn()
    const define = vi.fn()
    const debug = vi.fn()
    const acceptDiff = vi.fn()
    const restore = vi.fn()
    const references = vi.fn()
    render(
      <CodeWorkbench
        path="speak.go"
        content={'package bound\n\nfunc Speak() {\n\tfmt.Println("hi")\n}\n'}
        problems={[{ line: 4, message: 'undefined: fmt' }]}
        suggestion={'\treturn count'}
        stoppedLine={8}
        diff={'edited\n--- a.txt\n+++ a.txt\n-alpha\n+ALPHA\n--- b.txt\n+++ b.txt\n-beta\n+BETA\n'}
        onAccept={accept}
        onDefine={define}
        onDebug={debug}
        onAcceptDiff={acceptDiff}
        onRestore={restore}
        onReferences={references}
      />,
    )
    expect(screen.getByRole('list', { name: '问题' })).toHaveTextContent('undefined: fmt')
    expect(screen.getByText('断点停在第 8 行')).toBeInTheDocument()
    expect(screen.getByText(/--- a.txt/)).toBeInTheDocument()
    expect(screen.getByText(/--- b.txt/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: '接受补全' }))
    await userEvent.click(screen.getByRole('button', { name: '接受差异' }))
    await userEvent.click(screen.getByRole('button', { name: '还原差异' }))
    await userEvent.click(screen.getByRole('button', { name: '转到定义' }))
    await userEvent.click(screen.getByRole('button', { name: '查找引用' }))
    await userEvent.click(screen.getByRole('button', { name: '在此行停下' }))
    expect(accept).toHaveBeenCalledOnce()
    expect(acceptDiff).toHaveBeenCalledOnce()
    expect(restore).toHaveBeenCalledOnce()
    expect(define).toHaveBeenCalledOnce()
    expect(references).toHaveBeenCalledOnce()
    expect(debug).toHaveBeenCalledOnce()
  })
})
