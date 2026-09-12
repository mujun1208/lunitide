import { describe, expect, it } from 'vitest'
import { briefFieldLabel, briefLengthLabel, canFormalDeliver, conceptPreviewLabel, deferredOfficeCapabilitiesNotice, draftQualityNotice, findFactRefs, formalDeliverBlockedReason, qualityPromiseLabels, trialScopeNotice, visualScoreNotice } from './officeQualityUi'

describe('officeQualityUi', () => {
  it('disables formal deliver when blockers remain', () => {
    expect(
      canFormalDeliver({
        quality: 'partial',
        validations: [{ id: 'native_render', label: '渲染', status: 'unavailable', severity: 'blocking', message: '缺组件' }],
      }),
    ).toBe(false)
    expect(canFormalDeliver({ quality: 'passed', validations: [] })).toBe(true)
  })

  it('labels concept preview separately from rendered layout', () => {
    expect(conceptPreviewLabel(false)).toBe('概念预览')
    expect(conceptPreviewLabel(true)).toBe('文件排版预览')
  })

  it('keeps incomplete checks as drafts', () => {
    expect(draftQualityNotice('partial')).toContain('草稿')
  })

  it('defers enterprise templates and Univer collaboration', () => {
    expect(deferredOfficeCapabilitiesNotice()).toContain('本期不做')
    expect(deferredOfficeCapabilitiesNotice()).toContain('Univer')
    expect(deferredOfficeCapabilitiesNotice()).toContain('组织模板库')
  })

  it('lists fact refs from current preview nodes only', () => {
    expect(
      findFactRefs(
        [{ factId: 'orders', value: '1280', unit: '单' }],
        [{ id: 'n1', text: '订单 1280单' }, { id: 'n2', text: '其他' }],
      ),
    ).toEqual([{ factId: 'orders', nodeId: 'n1' }])
    expect(findFactRefs([{ factId: 'orders', value: '1280' }], [{ id: 'n1', text: '其他' }])).toEqual([])
    expect(findFactRefs([{ factId: 'x', value: '' }], [{ id: 'n1', text: '' }])).toEqual([])
  })

  it('lights quality promises only from existing evidence', () => {
    expect(
      qualityPromiseLabels({
        quality: 'partial',
        mode: 'imported',
        validations: [
          { id: 'structure', label: '结构检查', status: 'passed', severity: 'info', message: '结构完整' },
          { id: 'layout', label: '排版检查', status: 'unavailable', severity: 'warning', message: '排版组件尚未配置' },
        ],
      }),
    ).toEqual(['内容完整', '可继续编辑', '存在需处理的问题'])
    expect(qualityPromiseLabels(undefined)).toEqual([])
    expect(
      qualityPromiseLabels({
        quality: 'passed',
        mode: 'managed',
        validations: [
          { id: 'structure', label: '结构检查', status: 'passed', severity: 'info', message: '结构完整' },
          { id: 'layout', label: '排版检查', status: 'passed', severity: 'info', message: '排版通过' },
          { id: 'source', label: '来源', status: 'passed', severity: 'info', message: '有来源' },
        ],
      }),
    ).toEqual(['内容完整', '排版已检查', '关键数字有来源', '可继续编辑'])
  })

  it('explains why formal deliver stays disabled', () => {
    expect(formalDeliverBlockedReason({ quality: 'partial', validations: [] })).toContain('草稿')
    expect(
      formalDeliverBlockedReason({
        quality: 'partial',
        validations: [{ id: 'native_render', label: '渲染', status: 'unavailable', severity: 'blocking', message: '缺组件' }],
      }),
    ).toContain('渲染')
    expect(formalDeliverBlockedReason({ quality: 'passed', validations: [] })).toBe('')
  })

  it('states the trial scope without competitor claims', () => {
    expect(trialScopeNotice()).toContain('试验范围')
    expect(trialScopeNotice()).toContain('不声称')
    expect(trialScopeNotice()).not.toContain('已超过')
  })

  it('labels empty brief fields as unset instead of inventing defaults', () => {
    expect(briefFieldLabel('')).toBe('未填写')
    expect(briefFieldLabel('  客户  ')).toBe('客户')
    expect(briefLengthLabel(undefined)).toBe('页数未填写')
    expect(briefLengthLabel(0)).toBe('页数未填写')
    expect(briefLengthLabel(8)).toBe('约 8 页')
  })

  it('labels the rule visual score as uncalibrated and not a certification', () => {
    expect(visualScoreNotice()).toContain('未校准')
    expect(visualScoreNotice()).not.toContain('认证通过')
    expect(visualScoreNotice()).toContain('85')
  })
})
