export type VoicePath = 'cloud' | 'local' | 'omni' | 'volc'

export type ShownVoicePath = Exclude<VoicePath, 'omni'>

export type VoicePersona = {
  id: string
  name: string
  group: string
  gender: 'female' | 'male'
}

/** In-app 50-life catalogue. Local maps to GPT-SoVITS refpack IDs; MiniCPM-o uses the same cards as clone refs. */
export const VOICE_PERSONAS: VoicePersona[] = [
  { id: 'refpack:优质台湾腔.wav', name: '优质台湾腔', group: '台湾腔', gender: 'female' },
  { id: 'refpack:台湾男青年音故事讲述.wav', name: '台湾男青年', group: '台湾腔', gender: 'male' },
  { id: 'refpack:甜心少女.wav', name: '甜心少女', group: '甜美女声', gender: 'female' },
  { id: 'refpack:甜美萌妹.wav', name: '甜美萌妹', group: '甜美女声', gender: 'female' },
  { id: 'refpack:超嗲萌妹.wav', name: '超嗲萌妹', group: '甜美女声', gender: 'female' },
  { id: 'refpack:软糯萝莉.wav', name: '软糯萝莉', group: '甜美女声', gender: 'female' },
  { id: 'refpack:娇媚萝莉.wav', name: '娇媚萝莉', group: '甜美女声', gender: 'female' },
  { id: 'refpack:阳光甜心.wav', name: '阳光甜心', group: '甜美女声', gender: 'female' },
  { id: 'refpack:开朗妹妹.wav', name: '开朗妹妹', group: '温柔女声', gender: 'female' },
  { id: 'refpack:温暖御姐.wav', name: '温暖御姐', group: '温柔女声', gender: 'female' },
  { id: 'refpack:知性御姐.wav', name: '知性御姐', group: '温柔女声', gender: 'female' },
  { id: 'refpack:俏皮姐姐.wav', name: '俏皮姐姐', group: '温柔女声', gender: 'female' },
  { id: 'refpack:稚嫩少女.wav', name: '稚嫩少女', group: '温柔女声', gender: 'female' },
  { id: 'refpack:傲娇女王.wav', name: '傲娇女王', group: '气场女声', gender: 'female' },
  { id: 'refpack:冰山女王.wav', name: '冰山女王', group: '气场女声', gender: 'female' },
  { id: 'refpack:娇媚女神.wav', name: '娇媚女神', group: '气场女声', gender: 'female' },
  { id: 'refpack:青春女神.wav', name: '青春女神', group: '气场女声', gender: 'female' },
  { id: 'refpack:阳光少年.wav', name: '阳光少年', group: '个性男声', gender: 'male' },
  { id: 'refpack:冷面霸总.wav', name: '冷面霸总', group: '个性男声', gender: 'male' },
  { id: 'refpack:唠嗑大爷.wav', name: '唠嗑大爷', group: '个性男声', gender: 'male' },
  { id: 'refpack:云甜甜.wav', name: '云甜甜', group: '甜美少女', gender: 'female' },
  { id: 'refpack:偏萝莉的少女音.wav', name: '偏萝莉少女', group: '甜美少女', gender: 'female' },
  { id: 'refpack:可爱小萝莉.wav', name: '可爱小萝莉', group: '甜美少女', gender: 'female' },
  { id: 'refpack:甜美少女音.wav', name: '甜美少女音', group: '甜美少女', gender: 'female' },
  { id: 'refpack:撒娇小师妹.wav', name: '撒娇小师妹', group: '甜美少女', gender: 'female' },
  { id: 'refpack:萌小音（11岁 女）.wav', name: '萌小音', group: '甜美少女', gender: 'female' },
  { id: 'refpack:蛋黄（8岁 女孩）.wav', name: '蛋黄', group: '甜美少女', gender: 'female' },
  { id: 'refpack:叶子温柔师姐-中文.wav', name: '温柔师姐', group: '温柔御姐', gender: 'female' },
  { id: 'refpack:御姐.wav', name: '御姐', group: '温柔御姐', gender: 'female' },
  { id: 'refpack:温柔御妈.wav', name: '温柔御妈', group: '温柔御姐', gender: 'female' },
  { id: 'refpack:温软姐姐 柔和质感.wav', name: '温软姐姐', group: '温柔御姐', gender: 'female' },
  { id: 'refpack:女-温柔、冷酷、亦正亦邪.wav', name: '亦正亦邪', group: '温柔御姐', gender: 'female' },
  { id: 'refpack:中音磁性女声旁白.wav', name: '磁性旁白', group: '温柔御姐', gender: 'female' },
  { id: 'refpack:病娇反派女声.wav', name: '病娇反派', group: '气场古风', gender: 'female' },
  { id: 'refpack:青灯古佛皇后.wav', name: '青灯皇后', group: '气场古风', gender: 'female' },
  { id: 'refpack:邻家婶子.wav', name: '邻家婶子', group: '气场古风', gender: 'female' },
  { id: 'refpack:奶奶中文.wav', name: '奶奶', group: '气场古风', gender: 'female' },
  { id: 'refpack:中年男声（45岁±）.wav', name: '中年男声', group: '磁性男声', gender: 'male' },
  { id: 'refpack:大叔.wav', name: '大叔', group: '磁性男声', gender: 'male' },
  { id: 'refpack:大羊磁性舒适音.wav', name: '磁性舒适', group: '磁性男声', gender: 'male' },
  { id: 'refpack:磁性男声.wav', name: '磁性男声', group: '磁性男声', gender: 'male' },
  { id: 'refpack:质感叔音.wav', name: '质感叔音', group: '磁性男声', gender: 'male' },
  { id: 'refpack:纪录片宣传片高质男音.wav', name: '纪录片男音', group: '磁性男声', gender: 'male' },
  { id: 'refpack:少年侠客.wav', name: '少年侠客', group: '少年青年', gender: 'male' },
  { id: 'refpack:温柔青年音.wav', name: '温柔青年', group: '少年青年', gender: 'male' },
  { id: 'refpack:阳光有趣的学霸小哥.wav', name: '学霸小哥', group: '少年青年', gender: 'male' },
  { id: 'refpack:奶爸中文.wav', name: '奶爸', group: '少年青年', gender: 'male' },
  { id: 'refpack:若三夜（北京口音.wav', name: '北京青年', group: '少年青年', gender: 'male' },
  { id: 'refpack:掌门师叔、帝王高管.wav', name: '掌门师叔', group: '权威旁白', gender: 'male' },
  { id: 'refpack:老年人旁白（男）.wav', name: '老年旁白', group: '权威旁白', gender: 'male' },
]

export type VoicePathOption = {
  value: ShownVoicePath
  label: string
  badge: string
  kicker: string
  meta: string
  desc: string
}

/** Leftover MiniCPM-o saves still type as omni; the picker shows 云端. */
export function shownVoicePath(path: VoicePath): ShownVoicePath {
  if (path === 'local' || path === 'volc') return path
  return 'cloud'
}

/** Product picker for 月伴. MiniCPM-o stays in the type for leftover saves but is not offered. */
export const VOICE_PATHS: VoicePathOption[] = [
  {
    value: 'cloud',
    label: '云端',
    badge: '默认',
    kicker: '即开即用',
    meta: '晓晓 · 微软 Neural',
    desc: '免密钥。听写与朗读走现有云端通道，适合大多数对话。',
  },
  {
    value: 'volc',
    label: '火山',
    badge: '听写',
    kicker: 'seed-asr',
    meta: '火山听 · 晓晓读',
    desc: '听写走火山 seed-asr 2.0；朗读仍是晓晓。密钥配在供应商「语音模型」。',
  },
  {
    value: 'local',
    label: '本地',
    badge: '本机',
    kicker: '离线克隆',
    meta: 'sherpa + GPT-SoVITS',
    desc: '本机 sherpa 听写，GPT-SoVITS 克隆 50 种人生音色。音频不出设备。',
  },
]

export function voicePersonaGroups(personas = VOICE_PERSONAS): [string, VoicePersona[]][] {
  const map = new Map<string, VoicePersona[]>()
  for (const persona of personas) {
    const list = map.get(persona.group) ?? []
    list.push(persona)
    map.set(persona.group, list)
  }
  return [...map.entries()]
}

export function findVoicePersona(id: string): VoicePersona | undefined {
  return VOICE_PERSONAS.find(p => p.id === id)
}

export function omniPersonaCaption(id: string): string {
  const persona = findVoicePersona(id)
  return persona ? `人生：${persona.name}` : ''
}
