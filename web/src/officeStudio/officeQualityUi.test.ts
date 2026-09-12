import { describe, expect, it } from 'vitest'
import { briefFieldLabel, briefLengthLabel, canFormalDeliver, capabilityUsabilityLabels, conceptPreviewLabel, deferredOfficeCapabilitiesNotice, draftQualityNotice, findFactRefs, formalDeliverBlockedReason, generateActionNotice, importLimitNotice, nextLocateFactOffset, nextLocatePreviewOffset, officeCheckStatusLabel, qualityPromiseLabels, trialScopeNotice, usabilityScopeNotice, visualScoreNotice } from './officeQualityUi'

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
    expect(draftQualityNotice('passed')).toContain('尚未接受')
    expect(draftQualityNotice('passed', true)).toContain('已接受')
    expect(draftQualityNotice('passed')).not.toContain('可作为正式交付')
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

  it('walks preview pages when locating a node', () => {
    expect(nextLocatePreviewOffset('n2', [{ id: 'n1' }], 0, 20)).toEqual({ nextOffset: 20 })
    expect(nextLocatePreviewOffset('n1', [{ id: 'n1' }], 0, 20)).toEqual({ found: true })
    expect(nextLocatePreviewOffset('n9', [{ id: 'n1' }], 20)).toEqual({ missing: true })
  })

  it('walks preview pages when locating a locked fact', () => {
    const facts = [{ factId: 'orders', value: '1280', unit: '单' }]
    expect(nextLocateFactOffset(facts, [{ id: 'n1', text: '其他' }], 0, 20)).toEqual({ nextOffset: 20 })
    expect(nextLocateFactOffset(facts, [{ id: 'n2', text: '订单 1280单' }], 20, 40)).toEqual({ found: true, nodeId: 'n2' })
    expect(nextLocateFactOffset(facts, [{ id: 'n1', text: '其他' }], 40)).toEqual({ missing: true })
  })

  it('lights quality promises only from existing evidence', () => {
    expect(
      qualityPromiseLabels({
        quality: 'partial',
        mode: 'imported',
        validations: [
          { id: 'package', label: '文件结构与资源', status: 'passed', severity: 'info', message: '结构完整' },
          { id: 'geometry_bounds', label: '几何', status: 'passed', severity: 'info', message: '画布内' },
        ],
      }),
    ).toEqual(['内容完整', '可继续编辑', '存在需处理的问题'])
    expect(
      qualityPromiseLabels({
        quality: 'partial',
        mode: 'imported',
        validations: [
          { id: 'structure', label: '假结构', status: 'passed', severity: 'info', message: '假 id' },
          { id: 'layout', label: '假排版', status: 'passed', severity: 'info', message: '假 id' },
          { id: 'source', label: '假来源', status: 'passed', severity: 'info', message: '假 id' },
        ],
      }),
    ).toEqual(['可继续编辑', '存在需处理的问题'])
    expect(qualityPromiseLabels(undefined)).toEqual([])
    expect(
      qualityPromiseLabels(
        {
          quality: 'passed',
          mode: 'managed',
          validations: [
            { id: 'package', label: '文件结构与资源', status: 'passed', severity: 'info', message: '结构完整' },
            { id: 'native_render', label: '实际排版预览', status: 'passed', severity: 'info', message: '已渲染' },
          ],
        },
        {
          facts: [{ factId: 'orders', value: '1280', unit: '单' }],
          nodes: [{ id: 'n1', text: '订单 1280单' }],
        },
      ),
    ).toEqual(['内容完整', '排版已检查', '关键数字有来源', '可继续编辑'])
    expect(
      qualityPromiseLabels({
        quality: 'passed',
        mode: 'managed',
        validations: [{ id: 'pdf_structure', label: 'PDF 页面结构', status: 'passed', severity: 'info', message: '文件头' }],
      }),
    ).toEqual(['内容完整', '可继续编辑'])
    expect(
      qualityPromiseLabels(
        {
          quality: 'passed',
          mode: 'managed',
          validations: [{ id: 'package', label: '文件结构与资源', status: 'passed', severity: 'info', message: '结构完整' }],
        },
        { facts: [{ factId: 'orders', value: '1280', unit: '单' }], nodes: [] },
      ),
    ).toContain('关键数字有来源')
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

  it('states usability without claiming missing kernels', () => {
    expect(usabilityScopeNotice()).toContain('可用范围')
    expect(usabilityScopeNotice()).toContain('未配置')
    expect(usabilityScopeNotice()).toContain('草稿')
    expect(usabilityScopeNotice()).toContain('外部生成器未进生产主链')
    expect(usabilityScopeNotice()).toContain('企业审批')
    expect(usabilityScopeNotice()).toContain('在线协同')
    expect(usabilityScopeNotice()).not.toContain('已超过')
    expect(usabilityScopeNotice()).not.toContain('85 分认证')
  })

  it('labels optional tools from the current checks only', () => {
    expect(capabilityUsabilityLabels([])).toEqual(['PDF/A 未配置，草稿仍可用', '视觉模型未配置，草稿仍可用'])
    expect(
      capabilityUsabilityLabels([
        { id: 'pdfa', status: 'passed' },
        { id: 'visual-model', status: 'passed' },
      ]),
    ).toEqual(['PDF/A 已验证', '视觉模型已检查'])
    expect(
      capabilityUsabilityLabels([
        { id: 'pdfa', status: 'failed' },
        { id: 'visual-model', status: 'missing' },
      ]),
    ).toEqual(['PDF/A 未通过', '视觉模型已配置，待验渲染图'])
    expect(
      capabilityUsabilityLabels([
        { id: 'visual-model', status: 'passed' },
      ]).join(''),
    ).not.toContain('85')
  })

  it('labels every check status so the list is never blank', () => {
    expect(officeCheckStatusLabel('passed')).toBe('通过')
    expect(officeCheckStatusLabel('failed')).toBe('未通过')
    expect(officeCheckStatusLabel('blocked')).toBe('未通过')
    expect(officeCheckStatusLabel('missing')).toBe('待验')
    expect(officeCheckStatusLabel('unsupported')).toBe('未验证')
    expect(officeCheckStatusLabel('unavailable')).toBe('尚未验证')
    expect(officeCheckStatusLabel('pending')).toBe('待检查')
    expect(officeCheckStatusLabel('')).toBe('未验证')
    expect(officeCheckStatusLabel('unsupported', 'native_render')).toBe('缺组件，不能正式交付')
    expect(officeCheckStatusLabel('missing', 'fields_update')).toBe('缺组件，不能正式交付')
    expect(officeCheckStatusLabel('unsupported', 'target-powerpoint')).toBe('未验证')
  })

  it('states that target apps were not opened and export still works', () => {
    expect(
      capabilityUsabilityLabels([
        { id: 'target-powerpoint', status: 'unsupported' },
        { id: 'target-wps', status: 'unsupported' },
      ]),
    ).toContain('目标软件未做打开验证，仍可导出后自行打开')
    expect(capabilityUsabilityLabels([])).not.toContain('目标软件未做打开验证，仍可导出后自行打开')
  })

  it('states import cannot rebuild a brief and generate happens in chat', () => {
    expect(importLimitNotice()).toContain('不能从成稿反推')
    expect(importLimitNotice()).toContain('简报')
    expect(importLimitNotice()).not.toContain('已超过')
    expect(generateActionNotice()).toContain('对话')
    expect(generateActionNotice()).toContain('不是生成按钮')
    expect(generateActionNotice()).not.toContain('已超过')
  })

  it('does not treat a visual-model pass as an 85 certification', () => {
    expect(
      qualityPromiseLabels({
        quality: 'passed',
        mode: 'managed',
        validations: [{ id: 'visual-model', label: '视觉模型诊断', status: 'passed', severity: 'info', message: 'visualScore=uncalibrated' }],
      }),
    ).not.toContain('85 分认证')
  })
})
