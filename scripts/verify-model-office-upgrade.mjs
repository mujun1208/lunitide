import { createHash } from 'node:crypto'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const testdata = join(root, 'internal/modelquality/testdata/upgrade-v1')
const audit = join(root, 'docs/audits/model-office-upgrade')
const migrationsDir = join(root, 'migrations')
const allowedPkgs = ['./internal/modelquality', './internal/storage/sqlite']
const defaultRun = 'Test(RegressionSelectionRejectsZeroMatches|UpgradeMigrationBackupCompatibility)'
const expectedCommit = 'c93dd3f37bc17f34cb828ded121a14e5c098ed4c'
const stalePack = [/^0155_model_native/, /^0156_execution_contract/, /^0157_office_delivery/]

function fail(message, code = 1) {
  console.error(message)
  process.exit(code)
}

function readJSON(path) {
  return JSON.parse(readFileSync(path, 'utf8'))
}

function sha256Text(text) {
  return createHash('sha256').update(text).digest('hex')
}

function inventory() {
  const baseline = readJSON(join(audit, 'baseline.json'))
  const reserved = baseline.reservedMigrations ?? {}
  if (reserved.model_native !== '0157_model_native_v2.sql') {
    fail('reserved model_native must be remapped to 0157')
  }
  if (reserved.execution !== '0158_execution_contract_v2.sql') {
    fail('reserved execution must be remapped to 0158')
  }
  if (reserved.office !== '0159_office_delivery_v2.sql') {
    fail('reserved office must be remapped to 0159')
  }
  if (baseline.latestAppliedMigration !== '0156_project_factory.sql') {
    fail('baseline latest migration drifted')
  }
  if (baseline.commit !== expectedCommit) {
    fail('baseline commit must equal 0.4.81 HEAD')
  }
  if (baseline.traceabilityCommit !== baseline.commit) {
    fail('baseline.traceabilityCommit must equal baseline.commit')
  }
  if (baseline.layers?.E !== 'pending') {
    fail('layers.E must stay pending')
  }
  if (!/^[0-9a-f]{64}$/.test(baseline.workspace?.dirtyPathsDigest ?? '')) {
    fail('workspace.dirtyPathsDigest must be a sha256 hex digest')
  }
  if (!baseline.toolchain?.fonts || !baseline.toolchain?.libreOffice) {
    fail('toolchain.fonts and toolchain.libreOffice must be recorded')
  }

  const migrationNames = readdirSync(migrationsDir)
  for (const name of migrationNames) {
    if (stalePack.some((re) => re.test(name))) {
      fail(`stale pack filename present: ${name}`)
    }
  }
  if (!existsSync(join(migrationsDir, '0157_model_native_v2.sql'))) {
    fail('0157_model_native_v2.sql must exist after T02')
  }

  const suite = readJSON(join(testdata, 'eval-cases.json'))
  const cases = suite.cases ?? []
  const ids = new Set(cases.map((item) => item.caseId))
  if (ids.size !== 24) {
    fail(`expected 24 unique case IDs, got ${ids.size}`)
  }
  const digests = baseline.caseDigests ?? {}
  for (const item of cases) {
    const digest = sha256Text(JSON.stringify(item))
    if (digests[item.caseId] !== digest) {
      fail(`eval case digest missing or mismatched: ${item.caseId}`)
    }
  }

  const profiles = readJSON(join(testdata, 'profile-examples.json')).profiles ?? []
  if (profiles.length < 3) {
    fail(`profile-examples must have >=3 profiles, got ${profiles.length}`)
  }
  const hasDeepSeek = profiles.some((item) => item.family === 'deepseek')
  const hasGlmStandard = profiles.some((item) => item.family === 'glm' && item.endpointPurpose === 'standard')
  const hasGlmCoding = profiles.some((item) => item.family === 'glm' && /coding/i.test(item.endpointPurpose ?? ''))
  if (!hasDeepSeek || !hasGlmStandard || !hasGlmCoding) {
    fail('profile-examples must include DeepSeek + GLM standard + GLM Coding')
  }

  const trace = readJSON(join(audit, 'traceability.json'))
  if (trace.baselineCommit !== baseline.commit) {
    fail('traceability.json baselineCommit must equal baseline.json commit')
  }
  const fr = new Set((trace.requirements ?? []).map((item) => item.id))
  for (let i = 1; i <= 30; i++) {
    const id = `FR${String(i).padStart(2, '0')}`
    if (!fr.has(id)) {
      fail(`missing ${id}`)
    }
  }
  return { baseline, ids, fr }
}

const incompleteReleaseStatus = new Set(['', 'missing', 'pending', 'first_lock_only', 'inventory', 'inventory-only'])

function isGitSHA(value) {
  return /^[0-9a-f]{40}([0-9a-f]{24})?$/.test(String(value ?? '').toLowerCase())
}

function isDigest(value) {
  return /^[0-9a-f]{64}$/.test(String(value ?? '').toLowerCase())
}

function isPassResult(value) {
  const result = String(value ?? '').trim().toLowerCase()
  return result === 'pass' || result === 'passed'
}

function namedReleaseComplete(status) {
  const value = String(status ?? '').trim().toLowerCase()
  return value === 'complete' || value === 'passed' || value === 'certified' || value === 'live'
}

function assessReleaseEvidence() {
  const path = join(audit, 'release-evidence.json')
  if (!existsSync(path)) {
    fail('release-evidence.json is absent; --release fails closed')
  }
  const ev = readJSON(path)
  const reqs = ev.requirements
  if (!reqs || typeof reqs !== 'object' || Array.isArray(reqs)) {
    fail('release evidence requirements missing')
  }
  let complete = true
  for (let i = 1; i <= 30; i++) {
    const id = `FR${String(i).padStart(2, '0')}`
    const slot = reqs[id]
    if (slot == null || typeof slot !== 'object' || Array.isArray(slot) || Object.keys(slot).length === 0) {
      fail(`release evidence ${id} missing or empty`)
    }
    const status = String(slot.status ?? '').trim()
    if (status === 'inventory' || status === 'inventory-only') {
      fail(`release evidence ${id} is inventory-only`)
    }
    for (const key of ['implementationCommit', 'testCommand', 'testResult', 'runEvidenceDigest']) {
      if (!Object.prototype.hasOwnProperty.call(slot, key)) {
        fail(`release evidence ${id} missing ${key}`)
      }
    }
    if (
      incompleteReleaseStatus.has(status) ||
      !isGitSHA(slot.implementationCommit) ||
      !String(slot.testCommand ?? '').trim() ||
      !isPassResult(slot.testResult) ||
      !isDigest(slot.runEvidenceDigest)
    ) {
      complete = false
    }
  }
  if (!ev.modelQualification || typeof ev.modelQualification !== 'object') {
    fail('release evidence modelQualification missing')
  }
  if (!ev.templateCertificates || typeof ev.templateCertificates !== 'object') {
    fail('release evidence templateCertificates missing')
  }
  if (!ev.migrationRecord || typeof ev.migrationRecord !== 'object') {
    fail('release evidence migrationRecord missing')
  }
  if (!namedReleaseComplete(ev.modelQualification.status)) {
    complete = false
  }
  if (!namedReleaseComplete(ev.templateCertificates.status)) {
    complete = false
  }
  if (!namedReleaseComplete(ev.migrationRecord.status) || !String(ev.migrationRecord.latestAppliedMigration ?? '').trim()) {
    complete = false
  }
  if (ev.layers?.E === 'done' && !complete) {
    fail('layers.E=done without complete FR evidence')
  }
  return { complete, layerE: ev.layers?.E ?? '' }
}

function listTests() {
  const listed = spawnSync('go', ['test', ...allowedPkgs, '-list', '.'], {
    encoding: 'utf8',
    cwd: root,
  })
  if (listed.status !== 0) {
    fail(`go test -list failed: ${(listed.stderr || listed.stdout || '').trim()}`)
  }
  return (listed.stdout || '').split(/\r?\n/).map((line) => line.trim()).filter((line) => /^Test/.test(line))
}

function selectTests(names, pattern) {
  const selected = spawnSync('go', ['run', './internal/modelquality/cmd/selectreg', '-pattern', pattern], {
    encoding: 'utf8',
    cwd: root,
    input: names.join('\n') + '\n',
  })
  if (selected.status !== 0) {
    process.stderr.write(selected.stderr || selected.stdout || 'selectreg failed\n')
    process.exit(selected.status === 2 ? 2 : 1)
  }
  const namesOut = (selected.stdout || '').split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
  if (namesOut.length === 0) {
    fail('regression selection matched no tests')
  }
  return namesOut
}

function executeTests(pattern, expected) {
  const executed = spawnSync('go', ['test', ...allowedPkgs, '-json', '-run', pattern, '-count', '1'], {
    encoding: 'utf8',
    cwd: root,
  })
  const ran = new Set()
  const passed = new Set()
  const skipped = new Set()
  const failed = new Set()
  let packageFail = executed.status !== 0
  for (const line of (executed.stdout || '').split(/\r?\n/)) {
    if (!line.trim()) {
      continue
    }
    let event
    try {
      event = JSON.parse(line)
    } catch {
      continue
    }
    if (!event.Test) {
      if (event.Action === 'fail') {
        packageFail = true
      }
      continue
    }
    if (event.Action === 'run') {
      ran.add(event.Test)
    } else if (event.Action === 'pass') {
      passed.add(event.Test)
    } else if (event.Action === 'skip') {
      skipped.add(event.Test)
    } else if (event.Action === 'fail') {
      failed.add(event.Test)
    }
  }
  for (const name of expected) {
    if (failed.has(name)) {
      fail(`expected test failed: ${name}`)
    }
    if (skipped.has(name) && !passed.has(name)) {
      fail(`expected test skipped: ${name}`)
    }
    if (!ran.has(name) || !passed.has(name)) {
      fail(`expected test missing run+pass: ${name}`)
    }
  }
  if (expected.every((name) => skipped.has(name) && !passed.has(name))) {
    fail('regression selection was skip-only')
  }
  if (failed.size > 0 || packageFail) {
    fail(`go test failed: ${(executed.stderr || '').trim() || 'package or test failure'}`)
  }
  return expected
}

const args = process.argv.slice(2)
const inventoryOnly = args.includes('--inventory')
const releaseMode = args.includes('--release')
const runIdx = args.indexOf('--run')
const runPresent = runIdx >= 0
const runPattern = runPresent ? (args[runIdx + 1] ?? '') : defaultRun

const checked = inventory()

if (inventoryOnly) {
  console.log(JSON.stringify({ ok: true, mode: 'inventory' }, null, 2))
  process.exit(0)
}

if (releaseMode) {
  const evidence = assessReleaseEvidence()
  if (!evidence.complete) {
    fail('release evidence incomplete; inventory-only is not a release')
  }
  console.log(JSON.stringify({
    ok: true,
    mode: 'release',
    commit: checked.baseline.commit,
    layerE: evidence.layerE,
  }, null, 2))
  process.exit(0)
}

if (runPresent && runPattern === '') {
  console.error('regression selection is empty')
  process.exit(2)
}

const selected = selectTests(listTests(), runPattern)
const passed = executeTests(runPattern, selected)
console.log(JSON.stringify({
  ok: true,
  mode: 'execute',
  commit: checked.baseline.commit,
  passed,
  runPattern,
}, null, 2))
