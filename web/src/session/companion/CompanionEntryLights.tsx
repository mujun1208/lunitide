import type { CompanionEntryReport } from './companionLights'

export function CompanionEntryLights({
  lights,
  thinkReady,
}: {
  lights: CompanionEntryReport['lights']
  thinkReady?: boolean
}) {
  return (
    <div className="companion-lights" role="status" aria-label="听 说 想">
      {lights.map(light => {
        const state = light.key === 'think' && thinkReady === false ? 'off' : light.state
        const label = light.key === 'think' && thinkReady === false ? '未就绪' : light.label
        return (
          <span key={light.key} className={`companion-light ${state}`} data-light={light.key}>
            <i aria-hidden="true" />
            <b>{light.title}</b>
            <small>{label}</small>
          </span>
        )
      })}
    </div>
  )
}
