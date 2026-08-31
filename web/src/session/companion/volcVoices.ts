/** Official seed-tts-2.0 speakers. Must match internal/tts volcPresets. */

/** Stored 基础 URL. Engine appends Agent Plan ASR/TTS paths. */
export const VOLC_SPEECH_ORIGIN = 'https://openspeech.bytedance.com'

/** Agent Plan X-Api-Resource-Id for 豆包流式语音识别 2.0. */
export const VOLC_ASR_RESOURCE_ID = 'volc.seedasr.sauc.duration'

/** Agent Plan X-Api-Resource-Id for 豆包语音合成 2.0. Not a speaker token. */
export const VOLC_TTS_RESOURCE_ID = 'seed-tts-2.0'

export const VOLC_DEFAULT_VOICE_ID = 'zh_female_xiaohe_uranus_bigtts'

function foldTtsResourceToken(id: string): string {
  return id.trim().toLowerCase().replace(/_/g, '-').replace(/^doubao-/, '')
}

function foldAsrModelToken(id: string): string {
  return id.trim().toLowerCase().replace(/_/g, '-').replace(/^doubao-/, '')
}

/** Official full paths / ark text URLs collapse to the speech origin. */
export function canonicalizeVolcSpeechUrl(raw: string): string {
  const input = raw.trim()
  if (!input || input === 'https://' || input === 'http://') return VOLC_SPEECH_ORIGIN
  let href = input
  if (/^wss:/i.test(href)) href = `https:${href.slice(4)}`
  else if (/^ws:/i.test(href)) href = `http:${href.slice(3)}`
  let host = ''
  try {
    host = new URL(href).hostname.replace(/^\[|\]$/g, '').toLowerCase()
  } catch {
    throw new Error('火山语音基础 URL 只接受 https://openspeech.bytedance.com。官方文档的 wss/http 全路径不要整段当基础 URL；粘过来也会收成这个源。')
  }
  if (host === 'openspeech.bytedance.com' || host.endsWith('.volces.com') || host === 'volces.com') {
    return VOLC_SPEECH_ORIGIN
  }
  throw new Error('火山语音基础 URL 只接受 https://openspeech.bytedance.com。官方文档的 wss/http 全路径不要整段当基础 URL；粘过来也会收成这个源。')
}

export function canonicalizeVolcAsrModelId(id: string): string {
  const s = id.trim()
  if (/^volc\.(?:seedasr|bigasr)\./i.test(s)) return s
  const folded = foldAsrModelToken(s)
  if (/^seed-?asr(?:-[\d.]+)?$/.test(folded)) return VOLC_ASR_RESOURCE_ID
  return s
}

export function canonicalizeVolcTtsResourceId(id: string): string {
  const s = foldTtsResourceToken(id)
  if (s.startsWith('seed-tts-1') || s.startsWith('seedtts-1')) return 'seed-tts-1.0'
  if (isVolcTtsResourceId(id)) return VOLC_TTS_RESOURCE_ID
  return id.trim()
}

export function canonicalizeVolcModelId(kind: 'asr' | 'tts' | string, id: string): string {
  if (kind === 'asr') return canonicalizeVolcAsrModelId(id)
  if (kind === 'tts' && isVolcTtsResourceId(id)) return canonicalizeVolcTtsResourceId(id)
  return id.trim()
}

export type VolcOfficialSpeaker = {
  id: string
  name: string
  gender: 'female' | 'male'
  lang: string
  group: string
}

export const VOLC_OFFICIAL_SPEAKERS: readonly VolcOfficialSpeaker[] = [
  { id: 'zh_female_xiaohe_uranus_bigtts', name: '小何', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_vv_uranus_bigtts', name: 'Vivi', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_gaolengyujie_uranus_bigtts', name: '高冷御姐', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_qingxinnvsheng_uranus_bigtts', name: '清新女声', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_tianmeitaozi_uranus_bigtts', name: '甜美桃子', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_tianmeixiaoyuan_uranus_bigtts', name: '甜美小源', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_shuangkuaisisi_uranus_bigtts', name: '爽快思思', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_linjianvhai_uranus_bigtts', name: '邻家女孩', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_meilinvyou_uranus_bigtts', name: '魅力女友', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_female_liuchangnv_uranus_bigtts', name: '流畅女声', gender: 'female', lang: 'zh-CN', group: '通用女声' },
  { id: 'zh_male_m191_uranus_bigtts', name: '云舟', gender: 'male', lang: 'zh-CN', group: '通用男声' },
  { id: 'zh_male_taocheng_uranus_bigtts', name: '小天', gender: 'male', lang: 'zh-CN', group: '通用男声' },
  { id: 'zh_male_liufei_uranus_bigtts', name: '刘飞', gender: 'male', lang: 'zh-CN', group: '通用男声' },
  { id: 'zh_male_shaonianzixin_uranus_bigtts', name: '少年梓辛', gender: 'male', lang: 'zh-CN', group: '通用男声' },
  { id: 'zh_male_ruyayichen_uranus_bigtts', name: '儒雅逸辰', gender: 'male', lang: 'zh-CN', group: '通用男声' },
  { id: 'zh_female_cancan_uranus_bigtts', name: '知性灿灿', gender: 'female', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_female_sajiaoxuemei_uranus_bigtts', name: '撒娇学妹', gender: 'female', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_male_sunwukong_uranus_bigtts', name: '猴哥', gender: 'male', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_female_peiqi_uranus_bigtts', name: '佩奇猪', gender: 'female', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_male_dayi_uranus_bigtts', name: '大壹', gender: 'male', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_female_mizai_uranus_bigtts', name: '咪仔', gender: 'female', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_female_jitangnv_uranus_bigtts', name: '鸡汤女', gender: 'female', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_male_sophie_uranus_bigtts', name: '魅力苏菲', gender: 'male', lang: 'zh-CN', group: '角色配音' },
  { id: 'zh_female_yingyujiaoxue_uranus_bigtts', name: 'Tina老师', gender: 'female', lang: 'zh-CN', group: '教育客服' },
  { id: 'zh_female_kefunvsheng_uranus_bigtts', name: '暖阳女声', gender: 'female', lang: 'zh-CN', group: '教育客服' },
  { id: 'zh_female_xiaoxue_uranus_bigtts', name: '儿童绘本', gender: 'female', lang: 'zh-CN', group: '教育客服' },
  { id: 'en_male_tim_uranus_bigtts', name: 'Tim', gender: 'male', lang: 'en-US', group: '多语种' },
  { id: 'en_female_dacey_uranus_bigtts', name: 'Dacey', gender: 'female', lang: 'en-US', group: '多语种' },
  { id: 'en_female_stokie_uranus_bigtts', name: 'Stokie', gender: 'female', lang: 'en-US', group: '多语种' },
]

export function isVolcSpeakerId(id: string): boolean {
  return VOLC_OFFICIAL_SPEAKERS.some(s => s.id === id)
}

/** Agent Plan resource id, not a speaker token. Accepts seedtts-2.0 / doubao-seed-tts-2.0. */
export function isVolcTtsResourceId(id: string): boolean {
  const s = foldTtsResourceToken(id)
  return s === 'seed-tts' || s === 'seedtts' || s.startsWith('seed-tts-') || s.startsWith('seedtts-')
}

export function nextUnusedVolcSpeaker(usedIds: readonly string[]): VolcOfficialSpeaker | undefined {
  const used = new Set(usedIds)
  return VOLC_OFFICIAL_SPEAKERS.find(s => !used.has(s.id))
}
