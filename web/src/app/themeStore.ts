import{create}from'zustand'
import{readInitialLanguage,LANGUAGE_KEY,type Language}from'../i18n/language'
import type{Theme}from'./appTypes'

// A-02.1 themeStore (F-07): the single source of truth for cross-app theme +
// language. Per the migration plan, localStorage and document-element side
// effects are centralized here (the store is the only writer), so components no
// longer own this state via useState. The bridge-dependent nativeTheme.set side
// effect stays in App because it depends on an injectable bridge (tests inject
// a fake uiThemeBridge). Callable outside React via getState()/setState().
export const THEME_KEY='lunitide:theme'

function readInitialTheme():Theme{
 try{return localStorage.getItem(THEME_KEY)==='light'?'light':'dark'}catch{return'dark'}
}

// The store owns state + localStorage persistence (single writer). DOM element
// reflection and the injectable nativeTheme bridge stay in App's effect, so this
// module has no document/bridge coupling and is unit-testable in isolation.
function persistTheme(theme:Theme){
 try{localStorage.setItem(THEME_KEY,theme)}catch{/* storage-denied: state stays authoritative */}
}

function persistLanguage(language:Language){
 try{localStorage.setItem(LANGUAGE_KEY,language)}catch{/* storage-denied: state stays authoritative */}
}

export interface ThemeState{
 theme:Theme
 language:Language
 setTheme:(theme:Theme)=>void
 toggleTheme:()=>void
 setLanguage:(language:Language)=>void
 toggleLanguage:()=>void
}

export const useThemeStore=create<ThemeState>((set,get)=>({
 theme:readInitialTheme(),
 language:readInitialLanguage(),
 setTheme:theme=>{persistTheme(theme);set({theme})},
 toggleTheme:()=>{const next:Theme=get().theme==='dark'?'light':'dark';persistTheme(next);set({theme:next})},
 setLanguage:language=>{persistLanguage(language);set({language})},
 toggleLanguage:()=>{const next:Language=get().language==='zh-CN'?'en':'zh-CN';persistLanguage(next);set({language:next})},
}))

// hydrateThemeStore re-reads localStorage into the singleton store. App calls it
// on mount so behaviour matches the previous per-mount useState lazy init: a
// theme/language written to localStorage before render is restored, and tests
// (which clear localStorage in afterEach) get a fresh store per mount instead of
// leaking in-memory state across cases.
export function hydrateThemeStore(){
 useThemeStore.setState({theme:readInitialTheme(),language:readInitialLanguage()})
}