# Post-Restart Configuration and Contract Audit

Date: 2026-09-09, Asia/Shanghai. Read-only verification following the parent's
matching E2E host/engine/renderer deployment. No source edits, configuration or
credential mutations, staging, key rotation, restart, or upstream requests were
performed by this task. Only this audit and the original sync report were edited.

## Runtime and Database Evidence

- E2E desktop PID 53228, started 00:13:56; engine PID 59592, started 00:13:57,
  parent PID 53228. Both executable paths are under
  `E:/Trae-Work-Projects/lunitide/release/out/e2e-gui-20260908`.
- Production's recorded engine PID remains 14456 and is not alive. Process
  enumeration found only the E2E engine. No production engine was started.
- At 00:20:03, both actual databases passed `PRAGMA quick_check` using Node's
  SQLite `DatabaseSync` with `readOnly: true`, `query_only=ON`, and read
  transactions. No application bootstrap, migrations, or sync apply ran.
- At 00:21:53, authenticated `provider.list` from the E2E gateway matched all
  nine non-deleted database providers: IDs, names, protocols, base URLs,
  credential states, statuses, versions, and ordered model IDs/display names,
  kinds, defaults, vision flags and context windows. The named-pipe server PID
  was independently checked against 59592 before sending the local nonce.
  Legacy empty/voice kinds were normalized on both sides according to
  `internal/domain/provider/kind.go`; they are not target-model drift.

Actual database paths:

- `C:/Users/mujun/AppData/Local/Lunitide/lunitide.db`
- `C:/Users/mujun/AppData/Local/Lunitide-E2E/lunitide.db`

All four target provider rows remain `enabled`, `configured`, and
`openai_compatible`, with base URL `https://z.apiyihe.org/v1`.

| Profile | Provider | Version | Models | Loadable Bindings |
| --- | --- | ---: | --- | ---: |
| Lunitide | yiheAPI | 3 | doubao-seedream-5-0-260128; doubao-seedance-2-0-260128 | 1 |
| Lunitide | Deepseek-ocr | 1 | deepseek-ocr | 1 |
| Lunitide-E2E | yiheAPI | 3 | doubao-seedream-5-0-260128; doubao-seedance-2-0-260128 | 1 |
| Lunitide-E2E | Deepseek-ocr | 1 | deepseek-ocr | 1 |

Provider IDs remain `01M1ZB9VV7G7T5YJGCH28Z01RP` (yiheAPI) and
`01M1ZW7PJBRF4FN421V972Z377` (OCR). Both profiles retain the image provider
default and image kind default; video is not the provider default. The intended
profile-specific kind defaults remain different: production video=true,
OCR=false; E2E video=false, OCR=true. OCR remains its provider's default.
These differences preserve the original preferences, not an incomplete merge.

## Credential and Preservation Evidence

The existing helper was not rerun: even its inspect path calls secure-root/asset
ACL protection helpers. To keep this pass strictly read-only, an in-memory
PowerShell check reproduced the existing secret service's length-prefixed
SHA-256 binding, asset-name derivation and HMAC validation, then used Windows
CurrentUser DPAPI `Unprotect` with that entropy. All four bindings decrypted
to nonempty buffers. Corresponding credentials in the two profiles compared
equal in memory; no plaintext values, lengths, hashes, or refs were emitted.
Plaintext buffers and comparison copies were cleared in cleanup.

This demonstrates current binding/loadability, not a new call through
`secret.Service.WithSecret` or an upstream credential-success test. Ciphertext
hashes and ACLs before/after each asset read matched. Neither `Put` nor any
credential host submission/update path was called.

Current credential reference chains also match the original merge sources:
production yiheAPI against its production backup, and OCR/E2E against the E2E
backup. The copied production OCR binding is compared to its E2E source.
Both original transactional backups still have their recorded SHA-256:

- Production: `a138eff2fa3b8859abf088dc7227d67977254e820fea2d7f75554bd5b2918510`
- E2E: `bdb178cee41d405dc0a5fce68fcc280efb1ce00ab236cca4a3ddb071f019866c`

See [original sync evidence](../../_scratch/model-config-sync/RESULTS.md) for
backup paths and the original preservation checks. This pass does not assert
that unrelated sessions/settings remained globally unchanged during normal
application use; it neither read their contents nor rewrote them.

## Supplied Documents

Only the two requested pasted documents were read; no transcript, key input,
or application logs were inspected. Both fenced OpenAPI YAML documents parsed
with the existing `js-yaml` dependency. No referenced URLs were requested.

- OCR: `C:/Users/mujun/.codex/attachments/c5b70546-8fea-4bc0-83bc-e271365aaffc/pasted-text.txt`
  SHA-256 `97728bf7571f56375d132ee71e636c48ec7b081c40b7d6008d09d8c964765631`.
- Image: `C:/Users/mujun/.codex/attachments/bac31daf-5502-4761-bfdf-0116cff80a72/pasted-text.txt`
  SHA-256 `50ddfa08afa261658492e1d57a0fd73ade34ce12ce2d59ed8d368710ee815f29`.

### Existing Request Contract

- OCR: `POST /v1/chat/completions`, model `deepseek-ocr`, streaming, system
  prompt `<image>\nFree OCR.`, and user content containing an `image_url` with
  a PNG data URL. This matches the example's core contract. Diagnostics and
  the parent's key-probe add a text instruction for synthetic digits and
  `max_tokens=64`; the adapter also sends `stream_options.include_usage=true`.
  Source: `internal/app/provider_diagnostics.go:106`,
  `internal/llmadapter/openai.go:116` and `:232`.
- Image: `POST /v1/images/generations`, exact configured Seedream 5 model,
  `prompt`, `size: "2K"`, `response_format: "url"`, and no generic `n` field.
  The app sends JSON plus Bearer authorization. This matches the document's
  required fields and current-model size/example. Optional `output_format`,
  `watermark`, reference-image and group-generation fields are omitted; their
  documented defaults apply. Source: `internal/llmadapter/openai_media.go:15`
  and `:197`; URL joining: `internal/networkpolicy/client.go:230`.
- Neither document declares a server URL (`servers: []`). They describe
  relative routes, not a replacement origin/base URL. The saved `/v1` base
  remains appropriate; no endpoint suffix needs to be saved into the base.

### Document Inconsistencies

1. OCR schema/example contradiction: `messages[].content` is declared only
   as a string, but its own example uses a multimodal array. `tools` and
   `tool_choice` are marked required but absent from the OCR example. The
   example also contains a JavaScript-style comment and fails strict JSON
   parsing. These are documentation/schema defects, not evidence that the
   working request should add irrelevant tools or stringify image content.
2. OCR type/streaming specification defects: `temperature` and `top_p` are
   integers despite fractional values in their descriptions; `logit_bias` is
   typed as null despite its object description. The stream=true example and
   SSE description have only a nonstream `application/json` response schema,
   with no streaming response contract. Treat the schema as incomplete.
3. Image response schema gap: `b64_json` is advertised as a response option,
   but the response schema only defines and requires `url`. The current app
   requests URL output, so this does not conflict with that request.
4. Image authentication/metadata ambiguity: operation-level `security: []`
   disables the declared global Bearer scheme, while the optional header
   example suggests Bearer authentication. Its Qwen folder label and
   DALL-E/Azure response sample are unrelated template metadata, not proof of
   Seedream output or account access. The client correctly retains its key.
5. Image model-version/table ambiguity: several capability descriptions say
   `5.0-lite` while the model list and examples use `5-0-260128`. Some examples
   labeled jointly for 5.0/4.5/4.0 include `output_format`, described as 5.0-only.
   Older 1K/3.0 tables reverse the listed 4:3 and 3:4 dimensions. These do not
   invalidate the current 5.0 `2K` request, but are not reliable capability
   evidence for other versions.

### Remaining Compatibility Differences

These describe the source snapshot inspected during the initial post-restart
review, not subsequent parent-owned adapter edits. They are not classified as
proven causes of the parent's 503 results:

- OCR documentation marks `Accept: application/json` required. The actual
  adapter/connector sets JSON Content-Type and authorization, but no explicit
  Accept header. Its usage-stream extension is also absent from the pasted
  schema. Successful upstream acceptance of these details remains unverified.
- Image omission of `output_format` is allowed and the document defaults it to
  JPEG; however, `parseImageMedia` initially labels its result `image/png`
  (`internal/llmadapter/openai_media.go:251`). This is a response-metadata risk,
  not a request validation defect or an explanation for channel-unavailable.
  No successful response with the newly tested key exists to inspect here.

## Tests and Upstream Outcome

Eight selected existing local mock tests passed, without real credentials or
upstream requests (`go test`, `-count=1 -v -timeout=90s`):

- `internal/llmadapter`: `TestVisionPayloadContracts`,
  `TestUnavailableModelChannelGetsSafeSpecificDiagnosis`,
  `TestSeedreamImageUsesSupportedSize`, `TestOpenAIGenerateImageParsesURL`.
- `internal/app`: `TestVisionProbeIncludesSyntheticImageAndOCRPrompt`,
  `TestProviderDiagnosticsIdentifyUnavailableModelChannel`,
  `TestOCRProbeRejectsSuccessfulButIncorrectRecognition`,
  `TestProviderTestUsesSelectedMediaModelCapability`.

The first read-only comparison attempts stopped on a checker typo in the
protocol enum and on differently represented legacy model kinds. Correcting
only the in-memory checks to the source-defined enum/normalization yielded the
passing results above; no database or application-source correction was needed.

Latest actual upstream evidence remains parent-reported: the new key was read
through `key-probe` stdin without echo, and both OCR/image returned HTTP 503
`MODEL_CHANNEL_UNAVAILABLE`. The key was neither saved nor staged. Earlier
image evidence named group `svip` after the user's group change; the latest
new-key result does not independently establish its group. No key rotation is
authorized. The known failure is upstream channel availability, not proof of
invalid local storage, and does not certify every payload field beyond that
failure. OCR/image remain NOT verified/pass. Parent-reported completed video
evidence is unchanged and was not re-probed.

## Meeting Hardware Acceptance Boundary

Historical actual-hardware evidence from the
[multi-endpoint capture audit](2026-09-08-meetings-multi-endpoint-capture.md):
all four active outputs, including separate console/communications outputs,
passed generated-tone checks with original settings and while temporarily
muted; two simultaneous outputs passed. Original mute, master volume and all
default roles were restored/compared. PCM remained local/in memory; the native
fixtures did not open a microphone, join calls, or transmit audio.

Real Tencent/Feishu calls, their application-specific routing, live microphone
speech plus system audio, acoustic echo removal, and end-to-end recognition
remain unaccepted. Physical unplug/default-switch transitions were not tried;
recovery has deterministic fixture coverage, not gapless hardware acceptance.
Endpoint IDs are deduplicated, but mirrored signals across distinct devices or
user-enabled microphone monitoring may still duplicate/echo. Protected media,
exclusive-mode playback and driver-specific loopback limitations remain.

The older audit's undeployed status is historical: the parent now reports a
matching deployment and the above running process identities were verified.
No capture or hardware fixture was rerun against this restarted application,
and this task did not independently certify renderer/binary build provenance.
The original local hardware evidence must not be relabeled as real-call or
post-deployment end-to-end acceptance.

## Final Pause and Closure

The later GPT image document was read only:
`C:/Users/mujun/.codex/attachments/ef5804d4-fdc0-45df-a4ec-bdd9cbd56d73/pasted-text.txt`.
It specifies `POST /v1/images/generations` with example model
`gpt-image-2-all`, size `1024x1024` and `n: 1`. Its request schema omits the
example's `model` and `image` properties, while its response schema/example is
a chat completion with `choices`, not an image result. It therefore does not
establish an actual image response format or payload-size limit. No referenced
image URLs were fetched. Subsequent adapter edits belong to the parent and
were not changed or reverified by this task.

Final parent-reported outcome: both GPT image attempts returned HTTP 503 with
the Chinese diagnosis meaning "no available channel". No GPT model or new key
was saved. This is parent-supplied evidence, not a probe performed here. Existing
OCR/Seedream failures and the prior both-profile configuration sync are unchanged.

The user explicitly paused image/OCR work. No further model probes,
configuration mutations, credential staging or rotation are to be performed.
The verified post-restart database/credential evidence and historical meeting
hardware evidence above close this task within their stated limits; they do
not turn blocked upstream models or untested real meetings into passes.
This final closure changed only audit documents. No new runtime checks or tests
were run after the pause, and no source, database, key, or process was changed.
