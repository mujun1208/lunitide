import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ActionCard } from './ActionCard'

afterEach(cleanup)

const spec = {
  id: 'act-build',
  name: '构建',
  description: '在临时工作区内执行项目构建并产出日志。',
  argvPreview: ['npm', 'run', 'build', '--', '--mode', 'prod'],
  envAllowlist: ['NODE_ENV', 'CI'],
  cwdPolicy: 'workspace',
}

describe('ActionCard 权限与用途展示', () => {
  it('渲染动作名与用途描述', () => {
    render(<ActionCard spec={spec} />)
    expect(screen.getByText('构建')).toBeInTheDocument()
    expect(screen.getByText('在临时工作区内执行项目构建并产出日志。')).toBeInTheDocument()
  })

  it('argv 预览以代码块展示（空格连接）', () => {
    const { container } = render(<ActionCard spec={spec} />)
    expect(container.querySelector('.m5-argv')).toHaveTextContent('npm run build -- --mode prod')
  })

  it('envAllowlist 渲染为 chips', () => {
    render(<ActionCard spec={spec} />)
    expect(screen.getByText('NODE_ENV')).toBeInTheDocument()
    expect(screen.getByText('CI')).toBeInTheDocument()
    expect(screen.getAllByText(/^(NODE_ENV|CI)$/)).toHaveLength(2)
  })

  it('cwdPolicy=workspace 显示「工作区内」徽标', () => {
    render(<ActionCard spec={spec} />)
    expect(screen.getByText('工作区内')).toBeInTheDocument()
  })

  it('其他 cwdPolicy 原样显示', () => {
    render(<ActionCard spec={{ ...spec, cwdPolicy: 'isolated' }} />)
    expect(screen.getByText('isolated')).toBeInTheDocument()
  })

  it('空 envAllowlist 不渲染白名单行', () => {
    const { container } = render(<ActionCard spec={{ ...spec, envAllowlist: [] }} />)
    expect(container.querySelector('.m5-env-row')).toBeNull()
  })
})

describe('ActionCard 运行按钮', () => {
  it('点击触发 onRun', () => {
    const onRun = vi.fn()
    render(<ActionCard spec={spec} onRun={onRun} />)
    fireEvent.click(screen.getByTestId('m5-run-act-build'))
    expect(onRun).toHaveBeenCalledOnce()
  })

  it('disabled 时按钮禁用', () => {
    const onRun = vi.fn()
    render(<ActionCard spec={spec} onRun={onRun} disabled />)
    const btn = screen.getByTestId('m5-run-act-build') as HTMLButtonElement
    expect(btn).toBeDisabled()
    fireEvent.click(btn)
    expect(onRun).not.toHaveBeenCalled()
  })

  it('未提供 onRun 时不渲染运行按钮', () => {
    render(<ActionCard spec={spec} />)
    expect(screen.queryByTestId('m5-run-act-build')).toBeNull()
  })
})
