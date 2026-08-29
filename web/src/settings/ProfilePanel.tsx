import React, { useEffect, useRef, useState } from 'react'
import { getIdentityBridge, getPeopleBridge, type IdentityBridge, type PeopleBridge } from '../bridge/client'
import type { IdentityDTO } from '../generated/bridge'
import { ChoiceTiles } from './ChoiceTiles'
import { useZh } from '../i18n/language'

const STATUS_OPTIONS = [
  { value: 'online', label: '在线', desc: '局域网可见为在线' },
  { value: 'away', label: '离开', desc: '暂时不在座位上' },
  { value: 'busy', label: '忙碌', desc: '请勿轻易打扰' },
  { value: 'invisible', label: '隐身', desc: '发现开启时也不广播在线' },
] as const

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result === 'string' && reader.result.startsWith('data:image/')) {
        resolve(reader.result)
        return
      }
      reject(new Error('图片无法读取'))
    }
    reader.onerror = () => reject(new Error('图片无法读取'))
    reader.readAsDataURL(file)
  })
}

export function ProfilePanel({
  identity = getIdentityBridge(),
  people = getPeopleBridge(),
}: {
  identity?: IdentityBridge
  people?: PeopleBridge
}): React.JSX.Element {
  const zh = useZh()
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

  const publishAvatar = async (avatar: string) => {
    apply(await identity.update({ avatar }))
    setNotice('头像已更新')
  }

  const pickAvatar = async () => {
    if (busy) return
    setBusy(true)
    setNotice('')
    try {
      const picked = await people.filePick({ folder: false })
      if (!/\.(png|jpe?g|gif|webp|bmp)$/i.test(picked.fileName) && !/\.(png|jpe?g|gif|webp|bmp)$/i.test(picked.path)) {
        setNotice('请选择图片文件')
        return
      }
      await publishAvatar(picked.path)
    } catch (e) {
      const msg = e instanceof Error ? e.message : '头像更新失败'
      if (/取消/.test(msg)) {
        fileRef.current?.click()
        return
      }
      setNotice(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="profile-panel">
      {notice && <p className="profile-notice" role="status">{notice}</p>}

      <section className="setting-group profile-section" data-profile-section="identity" aria-labelledby="profile-identity-title">
        <h3 id="profile-identity-title" className="profile-section-title">身份</h3>
        <div className="profile-hero-card">
          <button type="button" className="profile-avatar" onClick={() => void pickAvatar()} aria-label="更换头像">
            {profile?.avatar ? <img src={profile.avatar} alt="" /> : <span>{nickname.trim().slice(0, 1) || '月'}</span>}
          </button>
          <input ref={fileRef} hidden type="file" accept="image/*" onChange={e => {
            const file = e.target.files?.[0]
            e.target.value = ''
            if (!file) return
            const nativePath = (file as File & { path?: string }).path
            void (nativePath ? publishAvatar(nativePath) : readFileAsDataURL(file).then(publishAvatar))
              .catch(err => setNotice(err instanceof Error ? err.message : '头像更新失败'))
          }} />
          <div className="profile-hero-copy">
            <p className="profile-kicker">本机身份</p>
            <p className="profile-hero-name">{nickname.trim() || '月汐用户'}</p>
            <small>{[orgName, department, title].filter(Boolean).join(' · ') || '还没有填写组织信息'}</small>
            <button type="button" className="profile-avatar-action" disabled={busy} onClick={() => void pickAvatar()}>更换头像</button>
          </div>
        </div>
        <p className="setting-desc profile-section-hint">这些字段会出现在局域网通讯录里。电脑控制仍然只作用于这台电脑，不会把桌面分享给同事。</p>
        <div className="profile-fields">
          <label>显示名<input value={nickname} maxLength={64} onChange={e => setNickname(e.target.value)} /></label>
          <label>组织<input value={orgName} maxLength={128} onChange={e => setOrgName(e.target.value)} placeholder="公司或团队" /></label>
          <label>部门<input value={department} maxLength={128} onChange={e => setDepartment(e.target.value)} placeholder="研发 / 设计 / …" /></label>
          <label>职位<input value={title} maxLength={128} onChange={e => setTitle(e.target.value)} /></label>
          <label className="wide">简介<textarea value={bio} maxLength={2000} rows={3} onChange={e => setBio(e.target.value)} placeholder="一句话介绍自己" /></label>
        </div>
        <div className="profile-actions">
          <button type="button" className="primary" disabled={busy || !nickname.trim()} onClick={() => void save()}>{busy ? '保存中…' : '保存名片'}</button>
        </div>
      </section>

      <section className="setting-group profile-section" data-profile-section="presence" aria-labelledby="profile-presence-title">
        <h3 id="profile-presence-title" className="profile-section-title">在场</h3>
        <ChoiceTiles legend="当前状态" legendClassName="sr-only" name="identity-status" value={profile?.status ?? 'online'} options={STATUS_OPTIONS} onChange={value => void setStatus(value)} />
      </section>

      <section className="setting-group profile-section" data-profile-section="lan" aria-labelledby="profile-lan-title">
        <h3 id="profile-lan-title" className="profile-section-title">局域网</h3>
        <div className="profile-stack-row">
          <div>
            <b>{zh ? '让同网段的月汐看见我' : 'Let others on this LAN see me'}</b>
            <div className="setting-desc">默认关闭。打开后用 UDP 广播昵称、部门和状态；发现到的人默认不信任，发文件必须对方确认。</div>
          </div>
          <button type="button" className={`profile-disc-toggle${profile?.discoveryEnabled ? ' on' : ''}`} disabled={busy} aria-pressed={!!profile?.discoveryEnabled} onClick={() => void people.discoverySet({ enabled: !profile?.discoveryEnabled }).then(d => profile && apply({ ...profile, discoveryEnabled: d.enabled })).catch(e => setNotice(e instanceof Error ? e.message : '无法切换发现'))}>
            {profile?.discoveryEnabled ? '已开启' : '已关闭'}
          </button>
        </div>
        <div className="profile-pin">
          <span>配对码</span>
          <b>{profile?.pairingCode ?? '------'}</b>
          <button type="button" disabled={busy} onClick={() => void identity.update({ regeneratePairingCode: true }).then(apply).catch(e => setNotice(e instanceof Error ? e.message : '无法更换配对码'))}>更换</button>
        </div>
      </section>

      <section className="setting-group profile-section" data-profile-section="security" aria-labelledby="profile-security-title">
        <h3 id="profile-security-title" className="profile-section-title">安全</h3>
        <p className="setting-desc profile-section-hint">只读。私钥永远不会出现在界面上。</p>
        <div className="profile-fields profile-id-fields">
          <label>subjectId<input readOnly value={profile?.subjectId ?? ''} /></label>
          <label className="wide">公钥<input readOnly value={profile?.publicKey ?? ''} /></label>
        </div>
        <p className="profile-security-sub">启动密码</p>
        <p className="setting-desc profile-section-hint">保护本机个人资料和同事记录。这不是传输加密，只锁住这台电脑上的身份写入。</p>
        {profile?.locked ? (
          <>
            <div className="profile-fields">
              <label className="wide">解锁<input type="password" value={unlock} onChange={e => setUnlock(e.target.value)} /></label>
            </div>
            <div className="profile-actions">
              <button type="button" onClick={() => void identity.unlock({ password: unlock }).then(apply).catch(e => setNotice(e instanceof Error ? e.message : '解锁失败'))}>解锁</button>
            </div>
          </>
        ) : (
          <>
            <div className="profile-fields">
              {profile?.passwordSet && <label>当前密码<input type="password" value={currentPassword} onChange={e => setCurrentPassword(e.target.value)} /></label>}
              <label className={profile?.passwordSet ? undefined : 'wide'}>新密码<input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder="留空则清除" /></label>
            </div>
            <div className="profile-actions">
              <button type="button" disabled={busy} onClick={() => void identity.passwordSet({ password, currentPassword }).then(next => { apply(next); setPassword(''); setCurrentPassword(''); setNotice(password ? '启动密码已更新' : '启动密码已清除') }).catch(e => setNotice(e instanceof Error ? e.message : '无法设置密码'))}>保存密码</button>
            </div>
          </>
        )}
      </section>
    </div>
  )
}
