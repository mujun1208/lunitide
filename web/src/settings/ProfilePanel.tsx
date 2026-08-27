import React, { useEffect, useRef, useState } from 'react'
import { getIdentityBridge, getPeopleBridge, type IdentityBridge, type PeopleBridge } from '../bridge/client'
import type { IdentityDTO } from '../generated/bridge'
import { ChoiceTiles } from './ChoiceTiles'

const STATUS_OPTIONS = [
  { value: 'online', label: '在线', desc: '局域网可见为在线' },
  { value: 'away', label: '离开', desc: '暂时不在座位上' },
  { value: 'busy', label: '忙碌', desc: '请勿轻易打扰' },
  { value: 'invisible', label: '隐身', desc: '发现开启时也不广播在线' },
] as const

async function shrinkAvatar(file: File): Promise<string> {
  const data = await file.arrayBuffer()
  const blob = new Blob([data], { type: file.type || 'image/png' })
  const url = URL.createObjectURL(blob)
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const el = new Image()
      el.onload = () => resolve(el)
      el.onerror = () => reject(new Error('头像无法读取'))
      el.src = url
    })
    const canvas = document.createElement('canvas')
    canvas.width = 96
    canvas.height = 96
    const ctx = canvas.getContext('2d')
    if (!ctx) throw new Error('无法处理头像')
    ctx.drawImage(img, 0, 0, 96, 96)
    const out = canvas.toDataURL('image/jpeg', 0.82)
    if (out.length > 65536) throw new Error('头像仍然过大，请换一张更小的图')
    return out
  } finally {
    URL.revokeObjectURL(url)
  }
}

export function ProfilePanel({
  identity = getIdentityBridge(),
  people = getPeopleBridge(),
}: {
  identity?: IdentityBridge
  people?: PeopleBridge
}): React.JSX.Element {
  const [profile, setProfile] = useState<IdentityDTO>()
  const [nickname, setNickname] = useState('')
  const [department, setDepartment] = useState('')
  const [title, setTitle] = useState('')
  const [orgName, setOrgName] = useState('')
  const [bio, setBio] = useState('')
  const [password, setPassword] = useState('')
  const [currentPassword, setCurrentPassword] = useState('')
  const [unlock, setUnlock] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)

  const load = async () => {
    try {
      const next = await identity.get()
      setProfile(next)
      setNickname(next.nickname)
      setDepartment(next.department)
      setTitle(next.title)
      setOrgName(next.orgName)
      setBio(next.bio)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '无法读取个人资料')
    }
  }

  useEffect(() => { void load() }, [identity])

  const apply = (next: IdentityDTO) => {
    setProfile(next)
    setNickname(next.nickname)
    setDepartment(next.department)
    setTitle(next.title)
    setOrgName(next.orgName)
    setBio(next.bio)
  }

  const save = async () => {
    if (!profile || busy) return
    setBusy(true)
    setNotice('')
    try {
      apply(await identity.update({ nickname: nickname.trim(), department, title, orgName, bio }))
      setNotice('个人资料已保存')
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const setStatus = async (status: IdentityDTO['status']) => {
    if (!profile || busy) return
    setBusy(true)
    try {
      apply(await identity.update({ status }))
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '状态更新失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="profile-panel">
      <section className="profile-hero setting-group">
        <div className="profile-hero-card">
          <button type="button" className="profile-avatar" onClick={() => fileRef.current?.click()} aria-label="更换头像">
            {profile?.avatar ? <img src={profile.avatar} alt="" /> : <span>{nickname.trim().slice(0, 1) || '月'}</span>}
          </button>
          <input ref={fileRef} hidden type="file" accept="image/*" onChange={e => {
            const file = e.target.files?.[0]
            e.target.value = ''
            if (!file) return
            void shrinkAvatar(file).then(async avatar => apply(await identity.update({ avatar }))).catch(err => setNotice(err instanceof Error ? err.message : '头像更新失败'))
          }} />
          <div>
            <p className="profile-kicker">本机身份</p>
            <h3>{nickname.trim() || '月汐用户'}</h3>
            <small>{[orgName, department, title].filter(Boolean).join(' · ') || '还没有填写组织信息'}</small>
          </div>
        </div>
        <p className="setting-desc">这些字段会出现在局域网通讯录里。电脑控制仍然只作用于这台电脑，不会把桌面分享给同事。</p>
      </section>

      <section className="setting-group">
        <div className="setting-group-title">名片</div>
        <div className="profile-fields">
          <label>显示名<input value={nickname} maxLength={64} onChange={e => setNickname(e.target.value)} /></label>
          <label>组织<input value={orgName} maxLength={128} onChange={e => setOrgName(e.target.value)} placeholder="公司或团队" /></label>
          <label>部门<input value={department} maxLength={128} onChange={e => setDepartment(e.target.value)} placeholder="研发 / 设计 / …" /></label>
          <label>职位<input value={title} maxLength={128} onChange={e => setTitle(e.target.value)} /></label>
          <label className="wide">简介<textarea value={bio} maxLength={2000} rows={3} onChange={e => setBio(e.target.value)} placeholder="一句话介绍自己" /></label>
        </div>
        <div className="dialog-actions profile-actions">
          <button type="button" className="primary" disabled={busy || !nickname.trim()} onClick={() => void save()}>{busy ? '保存中…' : '保存名片'}</button>
        </div>
      </section>

      <section className="setting-group">
        <ChoiceTiles legend="当前状态" name="identity-status" value={profile?.status ?? 'online'} options={STATUS_OPTIONS} onChange={value => void setStatus(value)} />
      </section>

      <section className="setting-group">
        <div className="setting-group-title">局域网发现</div>
        <div className="setting-row" style={{ gridTemplateColumns: '1fr auto' }}>
          <div>
            <b>让同网段的月汐看见我</b>
            <div className="setting-desc">默认关闭。打开后用 UDP 广播昵称、部门和状态；发现到的人默认不信任，发文件必须对方确认。</div>
          </div>
          <button type="button" disabled={busy} onClick={() => void people.discoverySet({ enabled: !profile?.discoveryEnabled }).then(d => profile && apply({ ...profile, discoveryEnabled: d.enabled })).catch(e => setNotice(e instanceof Error ? e.message : '无法切换发现'))}>
            {profile?.discoveryEnabled ? '已开启' : '已关闭'}
          </button>
        </div>
        <div className="profile-pin">
          <span>配对码</span>
          <b>{profile?.pairingCode ?? '------'}</b>
          <button type="button" disabled={busy} onClick={() => void identity.update({ regeneratePairingCode: true }).then(apply).catch(e => setNotice(e instanceof Error ? e.message : '无法更换配对码'))}>更换</button>
        </div>
      </section>

      <section className="setting-group">
        <div className="setting-group-title">本机标识</div>
        <p className="setting-desc">只读。私钥永远不会出现在界面上。</p>
        <div className="profile-fields">
          <label>subjectId<input readOnly value={profile?.subjectId ?? ''} /></label>
          <label className="wide">公钥<input readOnly value={profile?.publicKey ?? ''} /></label>
        </div>
      </section>

      <section className="setting-group">
        <div className="setting-group-title">启动密码</div>
        <p className="setting-desc">保护本机个人资料和同事记录。这不是传输加密，只锁住这台电脑上的身份写入。</p>
        {profile?.locked ? (
          <label>解锁<input type="password" value={unlock} onChange={e => setUnlock(e.target.value)} />
            <button type="button" onClick={() => void identity.unlock({ password: unlock }).then(apply).catch(e => setNotice(e instanceof Error ? e.message : '解锁失败'))}>解锁</button>
          </label>
        ) : (
          <div className="profile-fields">
            {profile?.passwordSet && <label>当前密码<input type="password" value={currentPassword} onChange={e => setCurrentPassword(e.target.value)} /></label>}
            <label>新密码<input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="留空则清除" /></label>
            <button type="button" disabled={busy} onClick={() => void identity.passwordSet({ password, currentPassword }).then(next => { apply(next); setPassword(''); setCurrentPassword(''); setNotice(password ? '启动密码已更新' : '启动密码已清除') }).catch(e => setNotice(e instanceof Error ? e.message : '无法设置密码'))}>保存密码</button>
          </div>
        )}
      </section>
      {notice && <p className="profile-notice" role="status">{notice}</p>}
    </div>
  )
}
