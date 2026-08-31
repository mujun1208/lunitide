import React, { useEffect, useState } from 'react'
import { getProviderBridge } from '../bridge/client'
import type { ModelDTO, ProviderDTO } from '../generated/bridge'
import { llmReadyProviders } from '../provider/modelKind'
import { loadMeetingSettings, saveMeetingSettings, type MeetingListen, type MeetingSettings } from '../meetings/meetingSettings'
import { useZh } from '../i18n/language'
import { ChoiceTiles } from './ChoiceTiles'

const LISTEN: Record<MeetingListen, { label: string; labelEn: string; desc: string; descEn: string }> = {
  cloud: {
    label: '系统',
    labelEn: 'System',
    desc: '浏览器自带听写，即开即用。只出实时字幕，不把系统声音送进识别。',
    descEn: 'Browser speech recognition. Live captions only; system audio is not sent into ASR.',
  },
  volc: {
    label: '火山',
    labelEn: 'Volcengine',
    desc: '已配置的火山 seed-asr。中文会议通常更稳，要在「模型与供应商」里先配语音模型。',
    descEn: 'Volcengine seed-asr. Usually stronger for Chinese meetings; configure a voice model first.',
  },
  local: {
    label: '本机',
    labelEn: 'This PC',
    desc: '本机 sherpa，听写不上传。可把系统声音混进识别；未装好则无法开始听写。',
    descEn: 'On-device sherpa. Can mix system audio. Refuses to start if sherpa is not ready.',
  },
}

export function MeetingNotesPanel({ onSaved }: { onSaved?: () => void }): React.JSX.Element {
  const zh = useZh()
  const [prefs, setPrefs] = useState<MeetingSettings>(() => loadMeetingSettings())
  const [choices, setChoices] = useState<Array<{ provider: ProviderDTO; model: ModelDTO }>>([])

  useEffect(() => {
    void getProviderBridge().list().then(listed => {
      const ready = llmReadyProviders(listed.items)
      setChoices(ready.flatMap(provider => provider.models.map(model => ({ provider, model }))))
    }).catch(() => undefined)
  }, [])

  const update = (patch: Partial<MeetingSettings>) => {
    setPrefs(current => {
      const next = saveMeetingSettings({ ...current, ...patch })
      onSaved?.()
      return next
    })
  }

  return (
    <div className="setting-group meeting-notes-settings">
      <div className="setting-group-title">{zh ? '听写与整理' : 'Listen and notes'}</div>
      <p className="setting-desc">
        {zh
          ? '听写只负责开会时的实时字幕。纪要模型只负责把已经转写出的字整理成摘要和待办。停止后的补转写固定走本机 sherpa，跟听写选择无关；换纪要模型也不会让乱码字幕变准。'
          : 'Listening is live captions only. The notes model only organizes already transcribed text. Catch-up after stop always uses this-PC sherpa.'}
      </p>
      <ChoiceTiles
        legend={zh ? '实时听写' : 'Live captions'}
        name="meeting-listen"
        value={prefs.listen}
        onChange={listen => update({ listen })}
        options={(['cloud', 'volc', 'local'] as MeetingListen[]).map(value => ({
          value,
          label: zh ? LISTEN[value].label : LISTEN[value].labelEn,
          desc: zh ? LISTEN[value].desc : LISTEN[value].descEn,
        }))}
      />
      <div className="setting-row">
        <div>
          <div className="setting-label">{zh ? '纪要模型' : 'Notes model'}</div>
          <div className="setting-desc">
            {zh
              ? '停止录制后，用这个对话模型把逐字稿整理成「会议摘要」和「决议/待办」。它不听写。留空则用已启用的默认对话模型。'
              : 'After you stop, this LLM turns the transcript into a summary and action items. It does not transcribe. Empty uses the default enabled chat model.'}
          </div>
        </div>
        <select className="setting-select" aria-label={zh ? '纪要模型' : 'Notes model'} value={prefs.modelId} onChange={e => update({ modelId: e.target.value })}>
          <option value="">{zh ? '自动（已启用的对话模型）' : 'Auto (enabled chat model)'}</option>
          {choices.map(item => (
            <option key={`${item.provider.id}:${item.model.modelId}`} value={item.model.modelId}>
              {item.model.displayName || item.model.modelId}
            </option>
          ))}
        </select>
      </div>
    </div>
  )
}
