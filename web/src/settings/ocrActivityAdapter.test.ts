import { expect, it } from 'vitest'
import { mapOCRPackOperation, mapOCRRun, OCR_SETTINGS_TARGET } from './ocrActivityAdapter'

it('OCR failure maps to the unique personal OCR detail', () => {
  const failed = mapOCRPackOperation({
    operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    phase: 'failed',
    errorCode: 'NO_VERIFIED_RUNTIME_PROFILE',
    retryable: false,
  })
  expect(failed.domain).toBe('ocr')
  expect(failed.phase).toBe('failed')
  expect(failed.terminal).toBe(true)
  expect(failed.successClaimable).toBe(false)
  expect(failed.unread).toBe(true)
  expect(failed.recoveryAction).toBe('open_settings')
  expect(failed.recoveryTarget).toEqual(OCR_SETTINGS_TARGET)
  expect(failed.recoveryTarget.settingsCategory).toBe('personal')
  expect(failed.recoveryTarget.settingsIntelligenceView).toBe('ocr')

  const succeeded = mapOCRPackOperation({
    operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    phase: 'succeeded',
  })
  expect(succeeded.successClaimable).toBe(true)
  expect(succeeded.unread).toBe(false)
  expect(succeeded.recoveryTarget).toEqual(OCR_SETTINGS_TARGET)

  const unknown = mapOCRRun({ runId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', phase: 'done' })
  expect(unknown.phase).toBe('uncertain')
  expect(unknown.successClaimable).toBe(false)
  expect(unknown.terminal).toBe(true)
  expect(unknown.recoveryTarget).toEqual(OCR_SETTINGS_TARGET)
})
