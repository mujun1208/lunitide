import { readFile, writeFile, mkdir, readdir } from 'node:fs/promises'
import { resolve, dirname, relative, sep } from 'node:path'
import { execFileSync } from 'node:child_process'
import Ajv2020 from 'ajv/dist/2020.js'
import addFormats from 'ajv-formats'

const root = resolve(import.meta.dirname, '..', '..')
const check = process.argv.includes('--check')
const schemaDirs = ['api/bridge/v1', 'api/rpc/v1']
const schemaPaths = (await Promise.all(schemaDirs.map(async dir => (await readdir(resolve(root, dir)))
  .filter(name => name.endsWith('.schema.json')).map(name => `${dir}/${name}`)))).flat().sort()
const schemas = await Promise.all(schemaPaths.map(async name => JSON.parse(await readFile(resolve(root, name), 'utf8'))))
const byName = new Map(schemaPaths.map((path, index) => [path.split('/').at(-1), schemas[index]]))
const envelope = byName.get('envelope.schema.json'), response = byName.get('response.schema.json')
const bridgeError = byName.get('error.schema.json'), handshake = byName.get('handshake.schema.json')
if (!envelope || !response || !bridgeError || !handshake) throw new Error('Missing core bridge/RPC schemas')

const ajv = new Ajv2020({ allErrors: true, strict: true, strictSchema: false })
addFormats(ajv)
for (const schema of schemas) ajv.addSchema(schema)
for (const schema of schemas) {
  const validate = ajv.getSchema(schema.$id)
  if (!validate) throw new Error(`Schema did not compile: ${schema.$id}`)
  const examples = schema['x-examples']
  if (!examples?.positive?.length || !examples?.negative?.length) throw new Error(`Schema needs x-examples positive and negative cases: ${schema.$id}`)
  for (const value of examples.positive) if (!validate(value)) throw new Error(`Positive example rejected: ${schema.$id}\n${ajv.errorsText(validate.errors)}`)
  for (const value of examples.negative) if (validate(value)) throw new Error(`Negative example accepted: ${schema.$id}`)
}

const props = schema => schema?.properties ?? {}
const required = schema => new Set(schema?.required ?? [])
const methodSchemas = schemas.filter(schema => schema['x-method']).sort((a, b) => a['x-method'].localeCompare(b['x-method']))
const methods = methodSchemas.map(schema => schema['x-method'])
const methodSchema = method => methodSchemas.find(schema => schema['x-method'] === method)
if (new Set(methods).size !== methods.length) throw new Error('Duplicate x-method metadata')
const envelopeMethods = props(envelope).method?.enum
if (JSON.stringify(envelopeMethods) !== JSON.stringify(methods)) throw new Error('Envelope method enum must exactly match sorted x-method schemas')
const providerList = methodSchemas.find(schema => schema['x-method'] === 'provider.list')
const publicSchema = byName.get('public.dto.schema.json')
const protocols = publicSchema?.$defs?.ProviderProtocol?.enum
if (!Array.isArray(protocols) || !protocols.length) throw new Error('Public ProviderProtocol enum is required')
const bridgeVersion = props(envelope).v.const
const rpcMajor = props(handshake).rpcMajor.const, rpcMinor = props(handshake).rpcMinor.maximum
const nonce = props(handshake).sessionNonce
const branches = response.oneOf
if (props(response).v.const !== bridgeVersion) throw new Error('Bridge versions differ')

const refEndsWith = (schema, suffix) => schema?.$ref?.endsWith(suffix)
const assert = (condition, message) => { if (!condition) throw new Error(`Bridge semantic assertion failed: ${message}`) }
const ulidRef = schema => refEndsWith(schema, '#/$defs/ULID')
const modelArray = schema => schema?.type === 'array' && schema.minItems === 1 && schema.maxItems === 50 && refEndsWith(schema.items, '#/$defs/ModelDTO')
const enabled = methodSchemas.filter(schema => schema['x-enabled']).map(schema => schema['x-method'])
assert(JSON.stringify(enabled) === JSON.stringify(['chat.start', 'project.create', 'project.list', 'provider.create', 'provider.credential.submit', 'provider.delete', 'provider.get', 'provider.list', 'provider.model.sync', 'provider.test', 'provider.update', 'session.create', 'session.list', 'stream.cancel', 'system.health']), 'enabled method set drift')
for (const method of ['session.create', 'session.list']) assert(ulidRef(props(methodSchema(method)).projectId), `${method}.projectId must be a ULID`)
assert(required(methodSchema('session.create')).has('title') && refEndsWith(methodSchema('session.create')['x-result'], '#/$defs/SessionDTO'), 'session.create contract drift')
assert(required(methodSchema('session.list')).has('projectId') && methodSchema('session.list')['x-result']?.properties?.items?.maxItems === 100, 'session.list contract drift')
assert(methodSchemas.every(schema => schema['x-owner'] === (['provider.credential.submit', 'diagnostics.export'].includes(schema['x-method']) ? 'host' : 'engine')), 'method ownership drift')
assert(refEndsWith(props(providerList).protocol, '#/$defs/ProviderProtocol'), 'provider.list protocol must explicitly reference ProviderProtocol')
assert(props(methodSchema('system.health')['x-result']).protocol?.const === bridgeVersion, 'system.health result protocol must be the Bridge version')
for (const method of ['provider.create', 'provider.update']) {
  const schema = methodSchema(method), fields = props(schema)
  for (const name of ['name', 'protocol', 'baseUrl', 'models']) assert(name in fields, `${method} must support ${name}`)
  assert(refEndsWith(fields.protocol, '#/$defs/ProviderProtocol'), `${method}.protocol must explicitly reference ProviderProtocol`)
  assert(modelArray(fields.models), `${method}.models must contain 1..50 ModelDTO values`)
  assert('credentialSubmissionId' in fields && 'status' in fields, `${method} must support credentialSubmissionId and status`)
}
assert(['name', 'protocol', 'baseUrl', 'models'].every(name => required(methodSchema('provider.create')).has(name)), 'provider.create required fields drift')
assert(required(methodSchema('provider.update')).has('expectedVersion'), 'provider.update requires expectedVersion')
for (const method of ['provider.get', 'provider.delete']) assert(ulidRef(props(methodSchema(method)).id), `${method}.id must be a ULID`)
const credentialSubmit = methodSchema('provider.credential.submit'), credentialFields = props(credentialSubmit), credentialResult = props(credentialSubmit['x-result'])
const submissionScopes = credentialFields.scope?.oneOf ?? []
assert(submissionScopes.length === 2 && ulidRef(props(submissionScopes[0]).providerId) && props(submissionScopes[1]).draftFingerprint, 'credential.submit needs provider or draft scope')
assert(credentialFields.credential?.maxLength === 61440, 'credential maximum must be 60 KiB')
assert(ulidRef(credentialResult.credentialSubmissionId) && credentialResult.expiresAt && credentialResult.expiresInSeconds?.maximum <= 300, 'credential.submit must return a short-TTL one-time submission')
assert(!('credentialState' in credentialResult) && !('credentialRef' in credentialResult), 'credential.submit result must not expose state or ref')
const syncResult = props(methodSchema('provider.model.sync')['x-result'])
assert(syncResult.models && syncResult.warnings && syncResult.version && props(methodSchema('provider.model.sync')).expectedVersion, 'model.sync requires models, warnings and CAS version data')
const testSchema = methodSchema('provider.test'), testResult = props(testSchema['x-result'])
assert('modelId' in props(testSchema) && ['status', 'stage', 'httpStatus', 'latencyMs', 'errorCode', 'sanitizedMessage', 'testedAt'].every(name => name in testResult), 'provider.test semantic fields drift')
const sensitiveNames = new Set(['apikey', 'secret', 'ciphertext', 'credentialref', 'credential'])
const scanSensitive = (schema, location, allowCredential = false) => {
  for (const [name, value] of Object.entries(props(schema))) {
    if (sensitiveNames.has(name.toLowerCase()) && !(allowCredential && name === 'credential')) throw new Error(`Sensitive field ${location}.${name} is forbidden`)
    scanSensitive(value, `${location}.${name}`)
  }
  if (schema?.items) scanSensitive(schema.items, `${location}[]`)
  for (const branch of schema?.oneOf ?? []) scanSensitive(branch, `${location}.oneOf`)
}
for (const [name, schema] of Object.entries(publicSchema?.$defs ?? {})) scanSensitive(schema, `public DTO.${name}`)
for (const schema of methodSchemas) {
  scanSensitive(schema['x-result'], `${schema['x-method']} result`)
  scanSensitive(schema, `${schema['x-method']} payload`, schema === credentialSubmit && schema['x-owner'] === 'host')
}

const json = JSON.stringify
const pascal = value => value.split(/[^a-zA-Z0-9]+/).map(part => part[0]?.toUpperCase() + part.slice(1)).join('')
const refName = ref => pascal(ref.split('/').at(-1).replace(/\.schema\.json$/, '').replace(/^#\/$defs\//, ''))
const tsType = (schema, name = '') => {
  if (!schema) return 'unknown'
  if (schema.$ref) return schema.$ref.includes('#/$defs/') ? pascal(schema.$ref.split('/').at(-1)) : refName(schema.$ref)
  if ('const' in schema) return json(schema.const)
  if (name === 'method') return 'BridgeMethod'
  if (schema.enum) return schema.enum.map(json).join(' | ')
  if (schema.oneOf) return schema.oneOf.map(item => tsType(item)).join(' | ')
  if (schema.type === 'array') return `Array<${tsType(schema.items)}>`
  if (schema.type === 'object' || schema.properties) {
    const fields = Object.entries(props(schema)).map(([key, value]) => `${json(key)}${required(schema).has(key) ? '' : '?'}: ${tsType(value, key)}`)
    return fields.length ? `{ ${fields.join('; ')} }` : schema.additionalProperties === false ? 'Record<string, never>' : 'object'
  }
  return schema.type === 'string' ? 'string' : ['integer', 'number'].includes(schema.type) ? 'number' : schema.type === 'boolean' ? 'boolean' : 'unknown'
}
const interfaceBody = schema => Object.entries(props(schema)).map(([name, value]) => `  ${name}${required(schema).has(name) ? '' : '?'}: ${tsType(value, name)}`).join('\n')
const publicTypes = Object.entries(publicSchema?.$defs ?? {}).filter(([name]) => !['CredentialState', 'ProviderProtocol'].includes(name)).map(([name, schema]) => `export type ${name} = ${tsType(schema)}\n`).join('')
const methodTypes = methodSchemas.map(schema => {
  const name = pascal(schema['x-method'])
  return `export type ${name}Payload = ${tsType(schema)}\nexport type ${name}Result = ${tsType(schema['x-result'])}\n`
}).join('')
const responseCommon = Object.entries(props(response)).filter(([name]) => !['ok', 'payload', 'error'].includes(name)).map(([name, value]) => `${name}: ${tsType(value, name)}`).join('; ')
const responseUnion = branches.map(branch => props(branch).ok.const ? `  | { ${responseCommon}; ok: true; payload: TPayload; error?: never }` : `  | { ${responseCommon}; ok: false; payload?: never; error: BridgeError }`).join('\n')
const credentialStates = publicSchema.$defs.CredentialState.enum
const ts = `// Code generated from discovered api/bridge/v1 and api/rpc/v1 schemas. DO NOT EDIT.\nexport const BRIDGE_METHODS = [${methods.map(json).join(', ')}] as const\nexport type BridgeMethod = (typeof BRIDGE_METHODS)[number]\nexport const PROVIDER_PROTOCOLS = [${protocols.map(json).join(', ')}] as const\nexport type ProviderProtocol = (typeof PROVIDER_PROTOCOLS)[number]\nexport const CREDENTIAL_STATES = [${credentialStates.map(json).join(', ')}] as const\nexport type CredentialState = (typeof CREDENTIAL_STATES)[number]\nexport const BRIDGE_VERSION = ${json(bridgeVersion)} as const\nexport const RPC_VERSION = { major: ${rpcMajor}, minor: ${rpcMinor} } as const\n\nexport interface BridgeRequest<TPayload extends object = object> {\n${interfaceBody(envelope).replace('v: "1.0"', 'v: typeof BRIDGE_VERSION').replace(/^  payload: .*$/m, '  payload: TPayload')}\n}\nexport interface BridgeError {\n${interfaceBody(bridgeError)}\n}\nexport type BridgeResponse<TPayload = unknown> =\n${responseUnion}\n${publicTypes}${methodTypes}export interface RpcHandshake {\n${interfaceBody(handshake)}\n}\n`

const methodConstants = methods.map(method => `\tMethod${pascal(method)} Method = ${json(method)}`).join('\n')
const metadata = methodSchemas.map(schema => `\tMethod${pascal(schema['x-method'])}: {Owner: ${json(schema['x-owner'])}, Enabled: ${schema['x-enabled']}},`).join('\n')
const goContractSource = `// Code generated from discovered bridge schemas. DO NOT EDIT.\npackage bridge\nconst Version = ${json(bridgeVersion)}\ntype Method string\nconst (\n${methodConstants}\n)\ntype MethodMetadata struct { Owner string; Enabled bool }\nvar MethodMetadataByMethod = map[Method]MethodMetadata{\n${metadata}\n}\nvar Methods = [...]Method{${methods.map(method => `Method${pascal(method)}`).join(', ')}}\nfunc ValidMethod(method string) bool { _, ok := MethodMetadataByMethod[Method(method)]; return ok }\n`
const goFields = schema => Object.keys(props(schema)).map(name => `${name}${required(schema).has(name) ? '' : ',omitempty'}`)
const goStrings = values => values.map(json).join(', ')
const publicJSON = JSON.stringify(publicSchema.$defs)
const goTestSource = `// Code generated from discovered bridge schemas. DO NOT EDIT.\npackage contract\nimport (\n "reflect"; "regexp"; "strings"; "testing"\n "github.com/lunitide/lunitide/internal/app"\n "github.com/lunitide/lunitide/internal/bridge"\n "github.com/lunitide/lunitide/internal/domain/provider"\n "github.com/lunitide/lunitide/internal/ipc"\n)\nfunc TestGeneratedSchemaConstantsMatchGoContracts(t *testing.T) {\n if bridge.Version != ${json(bridgeVersion)} { t.Fatalf("bridge version = %q", bridge.Version) }; if ipc.RPCMajor != ${rpcMajor} || ipc.RPCMinor != ${rpcMinor} { t.Fatal("RPC version drift") }\n if !reflect.DeepEqual([]string{string(provider.ProtocolOpenAICompatible), string(provider.ProtocolAnthropic)}, []string{${goStrings(protocols)}}) { t.Fatal("provider protocol drift") }\n}\nfunc TestGeneratedSchemaJSONFieldsMatchGoDTOs(t *testing.T) {\n assertJSONFields(t, reflect.TypeOf(bridge.Request{}), []string{${goStrings(goFields(envelope))}}); assertJSONFields(t, reflect.TypeOf(bridge.Error{}), []string{${goStrings(goFields(bridgeError))}}); assertJSONFields(t, reflect.TypeOf(bridge.Response{}), []string{${goStrings(goFields(response))}}); assertJSONFields(t, reflect.TypeOf(ipc.Handshake{}), []string{${goStrings(goFields(handshake))}})\n}\nfunc TestGeneratedEnabledEngineRuntimeRoutesMatchSchema(t *testing.T) {\n for method, metadata := range bridge.MethodMetadataByMethod { if metadata.Owner == "engine" && metadata.Enabled { if handler, ok := app.RuntimeHandlers[method]; !ok || handler == nil { t.Errorf("enabled engine method %q has no runtime handler", method) } } }\n for method := range app.RuntimeHandlers { metadata, ok := bridge.MethodMetadataByMethod[method]; if !ok || metadata.Owner != "engine" || !metadata.Enabled { t.Errorf("runtime method %q is not enabled engine contract", method) } }\n}\nfunc TestGeneratedPublicDTOsContainNoSensitiveFields(t *testing.T) {\n text := strings.ToLower(${json(publicJSON)}); for _, forbidden := range []string{"apikey", "secret", "ciphertext", "credentialref"} { if strings.Contains(text, forbidden) { t.Errorf("public DTO schema contains sensitive field %q", forbidden) } }\n}\nfunc TestGeneratedHandshakeNonceConstraint(t *testing.T) { expression := regexp.MustCompile(${json(nonce.pattern)}); valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"; if len(valid) != ${nonce.minLength} || !expression.MatchString(valid) || expression.MatchString(valid[:63]) { t.Fatal("nonce constraint drift") } }\nfunc assertJSONFields(t *testing.T, typ reflect.Type, want []string) { t.Helper(); got := make([]string, typ.NumField()); for i := range got { got[i] = typ.Field(i).Tag.Get("json") }; if !reflect.DeepEqual(got, want) { t.Fatalf("%s JSON fields = %v, want %v", typ, got, want) } }\n`
const gofmt = source => execFileSync('gofmt', [], { input: source, encoding: 'utf8' })
const outputs = new Map([[resolve(root, 'web/src/generated/bridge.ts'), ts], [resolve(root, 'internal/bridge/schema_generated.go'), gofmt(goContractSource)], [resolve(root, 'internal/contract/schema_generated_test.go'), gofmt(goTestSource)]])
let drift = false
for (const [target, content] of outputs) { if (check) { let current; try { current = await readFile(target, 'utf8') } catch {}; if (current !== content) { console.error(`Generated contract is stale: ${relative(root, target).split(sep).join('/')}`); drift = true } } else { await mkdir(dirname(target), { recursive: true }); await writeFile(target, content, 'utf8') } }
if (drift) process.exitCode = 1
