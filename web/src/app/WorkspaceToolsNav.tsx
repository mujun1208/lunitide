import React, { useEffect, useState } from 'react'
import { mcpBridge } from '../bridge/client'
import { leftoverArchivedMcp } from '../settings/leftoverMcp'
import { readProjectsOpen, SIDEBAR_PROJECTS_OPEN_KEY, writeSidebarFlag } from '../sidebarSplit'
import type { Page } from './appTypes'
import { NavIcon } from './navIcons'

export type WorkspaceLamp = 'ready' | 'current' | 'warn' | 'error' | 'off'

const LAMP_LABEL: Record<WorkspaceLamp, { zh: string; en: string }> = {
  ready: { zh: '可用', en: 'Ready' },
  current: { zh: '当前页', en: 'Current' },
  warn: { zh: '部分异常', en: 'Partial' },
  error: { zh: '失败', en: 'Failed' },
  off: { zh: '未用', en: 'Idle' },
}

function StatusLamp({ tone, zh }: { tone: WorkspaceLamp; zh: boolean }): React.JSX.Element {
  const label = zh ? LAMP_LABEL[tone].zh : LAMP_LABEL[tone].en
  return <i className={`nav-lamp nav-lamp-${tone}`} aria-hidden="true" title={label} />
}

const TOOLS: { page: Page; zh: string; en: string }[] = [
  { page: 'projects', zh: '项目管理', en: 'Project management' },
  { page: 'skill', zh: '技能中心', en: 'Skill Center' },
  { page: 'expert', zh: '专家中心', en: 'Expert Center' },
  { page: 'mcp', zh: 'MCP', en: 'MCP' },
  { page: 'plugins', zh: '插件', en: 'Plugins' },
  { page: 'assets', zh: '资产管理', en: 'Asset Management' },
]

export function WorkspaceToolsNav({
  page,
  setPage,
  zh,
}: {
  page: Page
  setPage: (page: Page) => void
  zh: boolean
}): React.JSX.Element {
  const [open, setOpen] = useState(readProjectsOpen)
  const [mcpLamp, setMcpLamp] = useState<WorkspaceLamp>('off')
  useEffect(() => {
    let alive = true
    const load = () => {
      void mcpBridge.list({}).then(result => {
        if (!alive) return
        const live = (result.endpoints ?? []).filter(item => item.state !== 'revoked' && leftoverArchivedMcp(item.args, item.url).length === 0)
        const failed = live.filter(item => item.state === 'quarantined' || item.state === 'degraded')
        const ready = live.filter(item => item.enabled && item.state === 'ready')
        if (failed.length && ready.length) setMcpLamp('warn')
        else if (failed.length) setMcpLamp('error')
        else if (ready.length) setMcpLamp('ready')
        else setMcpLamp('off')
      }).catch(() => {
        if (alive) setMcpLamp('off')
      })
    }
    load()
    const timer = window.setInterval(load, 4000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [page])
  const current = TOOLS.some(item => item.page === page)
  return (
    <section className={`office-group workspace-tools-group ${open ? 'is-open' : 'is-closed'}`}>
      <div className="conversation-directory">
        <button
          type="button"
          className={`office-heading${current ? ' is-current' : ''}`}
          aria-expanded={open}
          aria-controls="project-list"
          title={zh ? '绿灯可用，黄灯部分异常，红灯失败，灰灯未用' : 'Green ready, amber partial, red failed, gray idle'}
          onClick={() => setOpen(value => {
            const next = !value
            writeSidebarFlag(SIDEBAR_PROJECTS_OPEN_KEY, next)
            return next
          })}
        >
          <span aria-hidden="true">›</span>
          {zh ? '项目' : 'Projects'}
        </button>
        {open ? (
          <div id="project-list" className="office-nav-list workspace-tools-list">
            {TOOLS.map(item => {
              const tone: WorkspaceLamp = page === item.page
                ? 'current'
                : item.page === 'mcp' ? mcpLamp : 'ready'
              const label = zh ? item.zh : item.en
              return (
                <button
                  key={item.page}
                  type="button"
                  className={page === item.page ? 'active' : ''}
                  onClick={() => setPage(item.page)}
                  aria-label={label}
                >
                  <NavIcon name={item.page} />
                  <em className="nav-lamp-label">{label}</em>
                  <StatusLamp tone={tone} zh={zh} />
                </button>
              )
            })}
          </div>
        ) : null}
      </div>
    </section>
  )
}
