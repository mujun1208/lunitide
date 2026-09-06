export default {
 test: {
  environment: 'jsdom',
  include: ['docs/audits/2026-09-06-system-review/conversation-probes/voice-reproduction.test.ts'],
  maxWorkers: 1,
  testTimeout: 15000,
 }
}
