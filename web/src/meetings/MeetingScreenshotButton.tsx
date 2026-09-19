import React, { useEffect, useState } from 'react'
import { getCapabilityRolesBridge, type CapabilityRolesBridge } from '../bridge/client'
import { visionCapabilityGate, type CapabilityGate } from '../settings/capabilityGate'

export function MeetingScreenshotButton({
  roles,
}: {
  roles?: CapabilityRolesBridge
}): React.JSX.Element {
  const [gate, setGate] = useState<CapabilityGate>({ ready: false, reason: '未配置视觉能力' })
  useEffect(() => {
    let alive = true
    try {
      void (roles ?? getCapabilityRolesBridge()).get().then(got => {
        if (alive) setGate(visionCapabilityGate(got.roles))
      }).catch(() => {
        if (alive) setGate({ ready: false, reason: '未配置视觉能力' })
      })
    } catch {
      setGate({ ready: false, reason: '未配置视觉能力' })
    }
    return () => { alive = false }
  }, [roles])
  return (
    <button
      type="button"
      className="meeting-screenshot"
      disabled={!gate.ready}
      title={gate.reason || '会议截图'}
      aria-label="会议截图"
    >
      会议截图
    </button>
  )
}
