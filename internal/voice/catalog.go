package voice

import "fmt"

// What local recognition needs on disk, and where to get it.
//
// None of this ships in the installer. The runtime is 19 MB and the accurate
// model is 226 MB; putting either in the download every user takes would
// multiply the installer by twenty-five to serve a feature most of them will
// never turn on. So the installer stays small and this is fetched once, on
// the machine of whoever asks for it.
//
// Every URL here is pinned to an immutable revision and every file is
// checked against a digest recorded at the time this catalogue was written.
// Pinning matters more than usual: this is the one path in the product that
// downloads code from the internet and then executes it. A floating `main`
// would mean the bytes could change under a digest that no longer matched,
// turning every install into a failure, or — with no digest at all — into an
// unreviewed update. Both are worse than being out of date on purpose.

// ArchiveKind describes how a download is unpacked once it is verified.
type ArchiveKind int

const (
	// ArchiveNone stores the file as it arrived.
	ArchiveNone ArchiveKind = iota
	// ArchiveTarBz2 is a bzip2-compressed tar. Go decompresses bzip2 in the
	// standard library, so no external tool is needed.
	ArchiveTarBz2
)

// Download is one file to fetch, verify, and place.
type Download struct {
	// Path is where the bytes land, relative to the bundle directory. For an
	// archive it names the directory the members are extracted into.
	Path string
	// URLs are sources for the same file, tried in order until one answers.
	// Each must be immutable: a release asset or a revision-pinned blob.
	//
	// Mirrors are safe here precisely because the digest below pins the
	// content. A mirror cannot substitute anything — it can only serve the
	// bytes we already named, or fail. That turns "which host can this user
	// reach" into a question with several answers instead of one, which
	// matters a great deal when the first answer is huggingface.co and the
	// user is in China.
	URLs []string
	// SHA256 is the lowercase hex digest of the transferred file. An install
	// that does not match it is discarded, never used.
	SHA256 string
	// Bytes is the expected size, used to size the progress bar before the
	// first byte arrives and to reject a response whose length disagrees.
	Bytes int64
	// Archive selects unpacking, if any.
	Archive ArchiveKind
	// StripComponents drops leading path segments from archive members. The
	// sherpa tarballs wrap everything in one versioned directory that would
	// otherwise have to be named in every path that follows.
	StripComponents int
}

// BundleKind separates the engine from the things it loads.
type BundleKind string

const (
	// BundleRuntime is the recognizer program itself.
	BundleRuntime BundleKind = "runtime"
	// BundleModel is one set of acoustic model weights.
	BundleModel BundleKind = "model"
)

// Bundle is one independently installable unit.
type Bundle struct {
	// ID is the stable key: a directory name, a settings value, a bridge
	// payload field. Not prose, and not translated.
	ID string
	// Kind separates runtime from model.
	Kind BundleKind
	// Title and Detail are shown in the download prompt, in Chinese,
	// because that is what this product's users read.
	Title  string
	Detail string
	// Downloads are fetched in order.
	Downloads []Download
}

// TotalBytes is the transfer size, for a progress bar that does not jump.
func (b Bundle) TotalBytes() int64 {
	var total int64
	for _, d := range b.Downloads {
		total += d.Bytes
	}
	return total
}

// Bundle identifiers. Referenced from settings and bridge payloads, so they
// are part of the contract and cannot be renamed casually.
const (
	// RuntimeSherpa is the sherpa-onnx build the sidecar runs.
	RuntimeSherpa = "sherpa-onnx-1.13.6"
	// ModelParaformerZhEn is the accurate default: Mandarin and English in
	// one model, and robust on the regional accents a Chinese user is
	// likely to actually have.
	ModelParaformerZhEn = "streaming-paraformer-zh-en"
	// ModelZipformerZh14M is the small one, for a slow connection or a
	// machine with little room.
	ModelZipformerZh14M = "streaming-zipformer-zh-14m"
	// ModelOfflineParaformerZh is the non-streaming recognizer that produces
	// the text actually sent to the language model.
	ModelOfflineParaformerZh = "offline-paraformer-zh"
)

// DefaultModel is the streaming model, and its job is now the caption.
//
// It used to be the 226 MB bilingual Paraformer, chosen because the complaint
// that started this work was that the recognizer mis-heard. Measuring settled
// the question differently: on this repository's own recordings the streaming
// model hears 礼拜二 as 里拜二 and 频繁 as 平反, and no streaming model can do
// much better, because it has to commit to a word before it has heard the end
// of the sentence. The fix was not a bigger streaming model but a
// non-streaming one after the fact — see DefaultRefiner.
//
// Which leaves the streaming model doing what it is good at: showing the user
// that something is listening, and giving the endpointing rules a piece of
// text to judge. Both survive a rough transcript, because it is replaced by
// the accurate one before anybody acts on it. So the small model does, and
// the 200 MB goes to the recognizer whose output is believed.
const DefaultModel = ModelZipformerZh14M

// DefaultRefiner is the non-streaming recognizer run over the finished
// utterance, whose text is the one that reaches the language model.
//
// Measured against SenseVoice on the same four clips: this one reads 频繁 and
// 礼拜二 and inline English correctly where SenseVoice returns 平繁, 礼拜2 and
// OSOS, and it decodes at RTF 0.036 against 0.055. SenseVoice's one advantage
// is punctuation, which is worth less here than a content word: a language
// model reads unpunctuated Mandarin without trouble and cannot recover a word
// it was never given.
const DefaultRefiner = ModelOfflineParaformerZh

// The MinSizeRel build with the MSVC runtime linked statically (MT) and
// text-to-speech compiled out. Static linkage is the reason for the choice:
// the MD builds need the Visual C++ redistributable, and an app that asks a
// user to go install one has already lost them. TTS is excluded because this
// product has its own.
var runtimeSherpa = Bundle{
	ID:     RuntimeSherpa,
	Kind:   BundleRuntime,
	Title:  "本地语音识别引擎",
	Detail: "sherpa-onnx 1.13.6（Windows x64，约 19 MB）",
	Downloads: []Download{{
		Path:            ".",
		URLs:            []string{"https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.6/sherpa-onnx-v1.13.6-win-x64-shared-MT-MinSizeRel-no-tts.tar.bz2"},
		SHA256:          "14d2e4c7640c2ba9ad2bb5b249acb8159a4b855e32b0469c398c0ecb9f1f2fc9",
		Bytes:           19848412,
		Archive:         ArchiveTarBz2,
		StripComponents: 1,
	}},
}

// Only the int8 weights are fetched. The release tarball carries fp32
// alongside them and runs to 1 GB; the quantized pair is 226 MB and the
// accuracy difference is not audible in conversation.
var modelParaformer = Bundle{
	ID:     ModelParaformerZhEn,
	Kind:   BundleModel,
	Title:  "中英双语识别模型（推荐）",
	Detail: "Streaming Paraformer，支持普通话、英语及川/豫/津等口音，约 226 MB",
	Downloads: []Download{
		{
			Path:   "encoder.int8.onnx",
			URLs:   hfBlob(paraformerRepo, paraformerRevision, "encoder.int8.onnx"),
			SHA256: "81a70226a8934e6ed92aa1d4fc486b428b5398e2f2619ed4897b7294cab90e9a",
			Bytes:  165462184,
		},
		{
			Path:   "decoder.int8.onnx",
			URLs:   hfBlob(paraformerRepo, paraformerRevision, "decoder.int8.onnx"),
			SHA256: "f3cca9f77bb9d93c8fcbfb63ae617b6b1ee96818df3aa3b151c40658fe38594f",
			Bytes:  71664561,
		},
		{
			Path:   "tokens.txt",
			URLs:   hfBlob(paraformerRepo, paraformerRevision, "tokens.txt"),
			SHA256: "59aba8873a2ed1e122c25fee421e25f283b63290efbde85c1f01a853d83cb6e6",
			Bytes:  75756,
		},
	},
}

const (
	paraformerRepo     = "sherpa-onnx-streaming-paraformer-bilingual-zh-en"
	paraformerRevision = "8e40c43232a1c5c66c82111efc5820d3accca11b"
	zipformerRepo      = "sherpa-onnx-streaming-zipformer-zh-14M-2023-02-23"
	zipformerRevision  = "204ad334e2e683fd295359930cc16fc0432a23ac"
	offlineRepo        = "sherpa-onnx-paraformer-zh-2023-09-14"
	offlineRevision    = "def027084691107096b5ebba69785756d63de6c5"
)

// The recognizer whose transcript is believed.
//
// One file rather than the encoder/decoder pair the streaming models use: a
// non-streaming Paraformer is a single graph. Only the int8 weights are
// fetched, for the same reason as everywhere else here — the fp32 copy in the
// same repository is four times the size and the difference is not audible.
var modelOfflineParaformer = Bundle{
	ID:     ModelOfflineParaformerZh,
	Kind:   BundleModel,
	Title:  "中文精确识别模型",
	Detail: "Paraformer 非流式，说完一句后重新识别，约 232 MB",
	Downloads: []Download{
		{
			Path:   "model.int8.onnx",
			URLs:   hfBlob(offlineRepo, offlineRevision, "model.int8.onnx"),
			SHA256: "f36a0433bcf096bd6d6f11b80a3ac8bed110bdca632fe0d731df8d1a84475945",
			Bytes:  243371218,
		},
		{
			Path: "tokens.txt",
			URLs: hfBlob(offlineRepo, offlineRevision, "tokens.txt"),
			// The same vocabulary as the streaming bilingual model, byte for
			// byte. Not shared between the bundles anyway: a bundle that
			// depends on another bundle's files cannot be installed or
			// removed on its own.
			SHA256: "59aba8873a2ed1e122c25fee421e25f283b63290efbde85c1f01a853d83cb6e6",
			Bytes:  75756,
		},
	},
}

// Fourteen million parameters, Mandarin only, and it fits in the space the
// other model uses for its token table. Worth offering: a user on a metered
// connection who wants offline recognition at all is better served by this
// than by a 226 MB download they abandon.
var modelZipformer = Bundle{
	ID:     ModelZipformerZh14M,
	Kind:   BundleModel,
	Title:  "中文轻量识别模型",
	Detail: "Streaming Zipformer 14M，仅中文，约 24 MB",
	Downloads: []Download{
		{
			Path:   "encoder.int8.onnx",
			URLs:   hfBlob(zipformerRepo, zipformerRevision, "encoder-epoch-99-avg-1.int8.onnx"),
			SHA256: "1c556ea57cec304e55ec4b72e52c1cc098bb01476ed7d90f3de939fe126487b1",
			Bytes:  21621684,
		},
		{
			Path:   "decoder.int8.onnx",
			URLs:   hfBlob(zipformerRepo, zipformerRevision, "decoder-epoch-99-avg-1.int8.onnx"),
			SHA256: "22f123bb8cba9b38974b3df18a3f45e7081f4985ebb2e075d9f21f618c468bbf",
			Bytes:  1888682,
		},
		{
			Path:   "joiner.int8.onnx",
			URLs:   hfBlob(zipformerRepo, zipformerRevision, "joiner-epoch-99-avg-1.int8.onnx"),
			SHA256: "a7cf9d82757bdcf786059454495a9ca95e4bd7347f72473fc08d794475c36169",
			Bytes:  1795562,
		},
		{
			Path:   "tokens.txt",
			URLs:   hfBlob(zipformerRepo, zipformerRevision, "tokens.txt"),
			SHA256: "8b294db9045d6e5f94647f4c1eec1af4da143a75053c399611444b378ff966ac",
			Bytes:  48697,
		},
	},
}

// ModelArchitecture tells the sidecar which recognizer to construct, because
// the flags differ: a transducer has a joiner and a paraformer does not.
type ModelArchitecture string

const (
	// ArchParaformer is an encoder/decoder pair.
	ArchParaformer ModelArchitecture = "paraformer"
	// ArchTransducer is an encoder/decoder/joiner triple.
	ArchTransducer ModelArchitecture = "transducer"
	// ArchOfflineParaformer is the non-streaming Paraformer: one file, and
	// it sees the whole utterance before it commits to any of it.
	ArchOfflineParaformer ModelArchitecture = "offline-paraformer"
	// ArchSenseVoice is the multilingual non-streaming encoder.
	ArchSenseVoice ModelArchitecture = "sense-voice"
)

// Streaming reports whether an architecture can be fed audio as it arrives.
// The non-streaming ones are handed a finished utterance instead, and are
// driven by a different server binary.
func (a ModelArchitecture) Streaming() bool {
	return a == ArchParaformer || a == ArchTransducer
}

// Architecture reports how a model bundle is wired. Derived from the bundle
// rather than stored on it so a caller cannot describe a paraformer as a
// transducer and get a confusing failure three layers down.
func Architecture(bundleID string) (ModelArchitecture, error) {
	switch bundleID {
	case ModelParaformerZhEn:
		return ArchParaformer, nil
	case ModelZipformerZh14M:
		return ArchTransducer, nil
	case ModelOfflineParaformerZh:
		return ArchOfflineParaformer, nil
	}
	return "", fmt.Errorf("%w: %s", ErrUnknownBundle, bundleID)
}

// Models lists the installable models in the order a chooser should show
// them: recommended first.
func Models() []Bundle {
	return []Bundle{modelZipformer, modelParaformer, modelOfflineParaformer}
}

// StreamingModels lists the models a user actually chooses between.
//
// Only the streaming half is a choice. The refiner is not an alternative to
// these — it runs after them, on the finished utterance, and its text is what
// reaches the language model either way. Offering all three together would
// present a pipeline stage as if it were a competitor to another stage.
func StreamingModels() []Bundle {
	return []Bundle{modelZipformer, modelParaformer}
}

// IsStreamingModel reports whether a bundle ID names a caption model.
func IsStreamingModel(id string) bool {
	for _, b := range StreamingModels() {
		if b.ID == id {
			return true
		}
	}
	return false
}

// RequiredBundles lists everything local recognition needs on disk, in the
// order it should be downloaded: the engine, then the model that draws the
// caption, then the one that produces the text.
//
// Ordered so that a download interrupted halfway leaves the user with the
// cheap parts already done rather than 232 MB of a model they cannot run.
func RequiredBundles() []Bundle {
	return []Bundle{runtimeSherpa, modelZipformer, modelOfflineParaformer}
}

// Runtime is the engine bundle every model needs.
func Runtime() Bundle { return runtimeSherpa }

// LookupBundle finds a bundle by ID, runtime included.
func LookupBundle(id string) (Bundle, error) {
	if id == RuntimeSherpa {
		return runtimeSherpa, nil
	}
	for _, b := range Models() {
		if b.ID == id {
			return b, nil
		}
	}
	return Bundle{}, fmt.Errorf("%w: %s", ErrUnknownBundle, id)
}

// hfBlob builds revision-pinned Hugging Face download URLs, upstream first
// and the mainland mirror second.
//
// Pinning to a commit rather than a branch is what makes the recorded digests
// mean something a year from now. The mirror is here because huggingface.co
// is not reachable from much of China — a download that simply times out is
// how this feature would otherwise present itself to a large share of this
// product's users. hf-mirror.com serves the same paths, and since the digest
// is pinned it cannot serve anything else and be believed.
func hfBlob(repo, revision, file string) []string {
	path := "/csukuangfj/" + repo + "/resolve/" + revision + "/" + file
	return []string{"https://huggingface.co" + path, "https://hf-mirror.com" + path}
}
