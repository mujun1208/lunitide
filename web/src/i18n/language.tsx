import React, {createContext, useContext} from 'react'

export type Language = 'zh-CN' | 'en'

export const LANGUAGE_KEY = 'lunitide:language'
export const LANGUAGE_DEFAULT_EN_KEY = 'lunitide:language-default-en'

const LanguageContext = createContext<Language>('zh-CN')

export function readInitialLanguage(): Language {
  try {
    if (!localStorage.getItem(LANGUAGE_DEFAULT_EN_KEY)) {
      localStorage.setItem(LANGUAGE_DEFAULT_EN_KEY, '1')
      localStorage.setItem(LANGUAGE_KEY, 'en')
      return 'en'
    }
  } catch {
    return 'en'
  }
  return localStorage.getItem(LANGUAGE_KEY) === 'zh-CN' ? 'zh-CN' : 'en'
}

export function LanguageProvider({value, children}: {value: Language; children: React.ReactNode}): React.JSX.Element {
  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>
}

export function useLanguage(): Language {
  return useContext(LanguageContext)
}

export function useZh(): boolean {
  return useLanguage() === 'zh-CN'
}
