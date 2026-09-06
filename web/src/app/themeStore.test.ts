import{beforeEach,describe,expect,it}from'vitest'
import{useThemeStore,hydrateThemeStore,THEME_KEY}from'./themeStore'

describe('themeStore (F-07 A-02.1)',()=>{
 beforeEach(()=>{localStorage.clear();useThemeStore.setState({theme:'dark',language:'zh-CN'})})

 it('persists theme to localStorage as the single writer',()=>{
  useThemeStore.getState().setTheme('light')
  expect(useThemeStore.getState().theme).toBe('light')
  expect(localStorage.getItem(THEME_KEY)).toBe('light')
 })

 it('toggles theme between dark and light',()=>{
  useThemeStore.getState().toggleTheme()
  expect(useThemeStore.getState().theme).toBe('light')
  useThemeStore.getState().toggleTheme()
  expect(useThemeStore.getState().theme).toBe('dark')
 })

 it('toggles language between zh-CN and en',()=>{
  useThemeStore.getState().toggleLanguage()
  expect(useThemeStore.getState().language).toBe('en')
  expect(localStorage.getItem('lunitide:language')).toBe('en')
  useThemeStore.getState().toggleLanguage()
  expect(useThemeStore.getState().language).toBe('zh-CN')
 })

 it('hydrates theme from localStorage written before mount',()=>{
  localStorage.setItem(THEME_KEY,'light')
  hydrateThemeStore()
  expect(useThemeStore.getState().theme).toBe('light')
 })
})