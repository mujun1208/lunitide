import React, { useEffect, useState } from 'react'
import type { ActivitySnapshotDTO, SystemDiagnosticsResult } from '../generated/bridge'
import { ActivityCenter } from '../activity/ActivityCenter'
import { activityLampToneWithDiagnostics, activityLampWorstWithDiagnostics, type ActivityLampTone, type DiagnosticLampItem } from '../activity/activitySnapshot'
import { useNavStore } from '../app/navStore'
import { getSystemHealthBridge } from '../bridge/client'
import { useZh } from '../i18n/language'
import { useMediaStore } from '../media/MediaRuntime'
import { OCR_SETTINGS_TARGET } from './ocrActivityAdapter'

const TONE_CLASS: Record<ActivityLampTone, string> = {
  ready: 'nav-lamp-ready',
  warn: 'nav-lamp-warn',
  error: 'nav-lamp-error',
}

function Lamp({ tone, label }: { tone: ActivityLampTone; label: string }): React.JSX.Element {
  const meaning = tone === 'error' ? '故障' : tone === 'warn' ? '部分异常' : '正常'
  return (
    <span className="settings-run-lamp">
      <i className={`nav-lamp ${TONE_CLASS[tone]}`} aria-hidden="true" />
      <em className="nav-lamp-label">{label}</em>
      <span className="sr-only">{`${label} ${meaning}`}</span>
    </span>
  )
}

export function SettingsRunStatus({
  items,
  onRecover,
  diagnostics,
}: {
  items?: ActivitySnapshotDTO[]
  onRecover?: (item: ActivitySnapshotDTO) => void
  diagnostics?: DiagnosticLampItem[]
}): React.JSX.Element {
  const zh = useZh()
  const store = useMediaStore()
  const activities = items ?? store.activities
  const [diag, setDiag] = useState<DiagnosticLampItem[]>(diagnostics ?? [])
  useEffect(() => {
    if (diagnostics) {
      setDiag(diagnostics)
      return
    }
    if (items) return
    let alive = true
    try {
      void getSystemHealthBridge().diagnostics().then((result: SystemDiagnosticsResult) => {
        if (alive) setDiag(result.components)
      }).catch(() => {
        if (alive) setDiag([])
      })
    } catch {
      setDiag([])
    }
    return () => { alive = false }
  }, [diagnostics, items])
  const setPage = useNavStore(s => s.setPage)
  const setSettingsCategory = useNavStore(s => s.setSettingsCategory)
  const setSettingsIntelligenceView = useNavStore(s => s.setSettingsIntelligenceView)
  const setTarget = useNavStore(s => s.setTarget)
  const [open, setOpen] = useState(false)
  const overall = activityLampWorstWithDiagnostics(activities, diag)
  const recover = onRecover ?? ((item: ActivitySnapshotDTO) => {
    if (item.recoveryAction === 'open_settings') {
      setTarget(undefined)
      setSettingsCategory(OCR_SETTINGS_TARGET.settingsCategory)
      setSettingsIntelligenceView(OCR_SETTINGS_TARGET.settingsIntelligenceView)
      setPage('settings')
      return
    }
    if (item.recoveryAction === 'open_player') {
      setTarget(undefined)
      setPage('media')
      return
    }
    if (item.recoveryAction === 'retry' && item.retryable) {
      void store.playPause()
    }
  })
  const canExpand = overall !== 'ready'
  return (
    <div className="settings-run-status">
      <button
        type="button"
        className="settings-run-status-toggle"
        aria-expanded={open}
        aria-controls="settings-run-status-detail"
        disabled={!canExpand}
        onClick={() => {
          if (canExpand) setOpen(value => !value)
        }}
      >
        <span className="settings-run-status-title">{zh ? '运行状态' : 'Run status'}</span>
        <Lamp tone={overall} label={zh ? '总览' : 'All'} />
        <Lamp tone={activityLampToneWithDiagnostics(activities, 'tool', diag)} label={zh ? '工具' : 'Tools'} />
        <Lamp tone={activityLampToneWithDiagnostics(activities, 'ocr', diag)} label="OCR" />
        <Lamp tone={activityLampToneWithDiagnostics(activities, 'media', diag)} label={zh ? '媒体' : 'Media'} />
      </button>
      {open ? (
        <div id="settings-run-status-detail">
          <ActivityCenter items={activities} open onClose={() => setOpen(false)} onRecover={item => {
            recover(item)
            setOpen(false)
          }} />
        </div>
      ) : null}
    </div>
  )
}
