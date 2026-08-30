import { useEffect, useState } from 'react'
import {
  loadUserAsrCorrectionText,
  saveUserAsrCorrectionText,
} from '../session/companion/asrCorrections'

export function AsrCorrectionRow(): React.JSX.Element {
  const [text, setText] = useState(loadUserAsrCorrectionText)
  useEffect(() => {
    setText(loadUserAsrCorrectionText())
  }, [])
  return (
    <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
      <div>
        <div className="setting-label">识别纠错</div>
        <div className="setting-desc">
          听写结果送模型前按「误识别 : 正确」替换整词（大小写不敏感）。内置已覆盖月汐近音、桌面、汽水音乐、GPT-SoVITS / WebView2 等；这里只写你还需要的。
        </div>
      </div>
      <textarea
        className="setting-input"
        aria-label="识别纠错"
        rows={4}
        spellCheck={false}
        placeholder={'open cloud : OpenClaw\n贾维尔 : 贾维斯'}
        value={text}
        onChange={event => {
          const next = event.target.value
          setText(next)
          saveUserAsrCorrectionText(next)
        }}
        style={{ width: '100%', minHeight: 72, fontFamily: 'var(--mono)', fontSize: 12, resize: 'vertical' }}
      />
    </div>
  )
}
