package omni

import "github.com/lunitide/lunitide/internal/voice"

// MiniCPM-o 4.5 Q4 lives in the product data directory, not the installer.
// Same rule as local ASR: the app stays small, whoever turns this channel
// on fetches the GGUF weights once. No API key — inference is loopback.

const (
	// BundleID is the on-disk directory name under the omni data root.
	BundleID = "minicpm-o-4_5-q4"
	// HuggingFaceRepo is the public GGUF snapshot (Apache-2.0).
	HuggingFaceRepo = "openbmb/MiniCPM-o-4_5-gguf"
	// Revision pins the catalogue digests. Do not float on main.
	Revision = "db25077c33951fe163b42986fba0132e279872a2"
	// LLMFile is the Q4_K_M checkpoint llama-omni-server loads.
	LLMFile = "MiniCPM-o-4_5-Q4_K_M.gguf"
	// ListenAddr is the only bind we ever spawn. Remote omni is out of scope.
	ListenAddr = "127.0.0.1:19080"
	// HTTPProbe is the matching HTTP origin for liveness.
	HTTPProbe = "http://127.0.0.1:19080"
	// SampleRate is the PCM rate the companion capture already produces.
	SampleRate = 16000
	// ChunkSamples is one duplex prefill window (1 second).
	ChunkSamples = SampleRate
	// ChunkBytes is int16 mono for one chunk.
	ChunkBytes = ChunkSamples * 2
)

// ModelBundle is the Q4 snapshot the settings download button fetches.
func ModelBundle() voice.Bundle {
	return voice.Bundle{
		ID:     BundleID,
		Kind:   voice.BundleModel,
		Title:  "MiniCPM-o 4.5 Q4",
		Detail: "本机全双工多模态语音模型（Q4_K_M），约 8 GB，下载进月汐数据目录，无需 API Key",
		Downloads: []voice.Download{
			hfFile(LLMFile, 5026714400, "1237a97ee081b8abebc47aa7dad565701e8f5f904cdc92f6723ac4281bbc0932"),
			hfFile("audio/MiniCPM-o-4_5-audio-F16.gguf", 660167904, "d5b188ac7feaf98e17175c3f9bd14bf269301bfd187439fdaa3e3a494fc32ef7"),
			hfFile("tts/MiniCPM-o-4_5-projector-F16.gguf", 14948640, "4b1b5b377358a5e594a304ff6ea5d52df606a9ba7d886c4299d232f0c67dd1fd"),
			hfFile("tts/MiniCPM-o-4_5-tts-F16.gguf", 1157244416, "c7be3748a863dd6966ae7eed42600b7f41ca67affb03729ff245247f0e5ea088"),
			hfFile("token2wav-gguf/encoder.gguf", 151339008, "7f8d265da594eaf5e1de2db8f5f1867dbcb0bb75ef5878fadf2952347116f4d0"),
			hfFile("token2wav-gguf/flow_extra.gguf", 13663328, "c67611aa7d02500fe395a7798bf0bfdfb55c74d37ba93934ca74d82b4e63f78d"),
			hfFile("token2wav-gguf/flow_matching.gguf", 458250240, "eda6069f3edeb5dd3a87fbf2aedb2ddd1b46f3273926c4fcf09b24476a39cab8"),
			hfFile("token2wav-gguf/hifigan2.gguf", 83242816, "1b8b3bf5d8d3066aeee4324fdcdd41aefce170d0ee907645858de408d82835c2"),
			hfFile("token2wav-gguf/prompt_cache.gguf", 211613152, "81fe6f541ebe0b67db06a1e395df928da47991dc9637c2cf47d6c59d5b979f2c"),
			hfFile("vision/MiniCPM-o-4_5-vision-F16.gguf", 1095113184, "1453678cc4e4fe18de241952962e234f265cb8dda780773526103ab8ba82f421"),
		},
	}
}

const (
	// RuntimeRevision pins the Windows Comni tree we vendor llama-omni-server
	// from at packaging time. Do not float on latest.
	RuntimeRevision = "v1.0.22"
	// RuntimeSetupFile is the versioned NSIS installer used only by
	// release/Publish-OmniRuntime.ps1 to unpack the slim runtime zip.
	RuntimeSetupFile = "Comni-Setup-1.0.22-win64.exe"
	RuntimeSHA256    = "72cefacba846920c3063479bc4bbfcdc268bb494623ee84f5f1e57464202a514"
	RuntimeBytes     = 564212994
)

// RuntimeBundle pins the Comni NSIS installer that packaging unpacks. The
// running product does not download this file — users get llama-omni-server
// from the staged omni/llama-omni-runtime.zip, and MiniCPM-o weights from
// ModelBundle.
func RuntimeBundle() voice.Bundle {
	path := "/tc-mb/llama.cpp-omni/releases/download/" + RuntimeRevision + "/" + RuntimeSetupFile
	return voice.Bundle{
		ID:     "comni-runtime-" + RuntimeRevision,
		Kind:   voice.BundleRuntime,
		Title:  "llama-omni-server",
		Detail: "打包时从 Comni 解开的本机 MiniCPM-o 推理进程，随月汐安装，不含 8 GB GGUF 权重",
		Downloads: []voice.Download{
			{
				Path: RuntimeSetupFile,
				URLs: []string{
					"https://github.com" + path,
					"https://gh-proxy.com/https://github.com" + path,
				},
				SHA256: RuntimeSHA256,
				Bytes:  RuntimeBytes,
			},
		},
	}
}

func hfFile(name string, bytes int64, sha256 string) voice.Download {
	path := "/" + HuggingFaceRepo + "/resolve/" + Revision + "/" + name
	return voice.Download{
		Path:   name,
		URLs:   []string{"https://huggingface.co" + path, "https://hf-mirror.com" + path},
		SHA256: sha256,
		Bytes:  bytes,
	}
}
