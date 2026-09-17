package ocrapp

// PP-OCR is fetched on demand, the same way local speech is: nothing ships
// in Setup, the catalogue is pinned, and the digest is checked before the
// bytes are executed.

type ArchiveKind int

const (
	ArchiveNone ArchiveKind = iota
	ArchiveZip
)

type Download struct {
	Path            string
	URLs            []string
	SHA256          string
	Bytes           int64
	Archive         ArchiveKind
	StripComponents int
}

type Bundle struct {
	ID        string
	Title     string
	Detail    string
	Downloads []Download
}

func (b Bundle) TotalBytes() int64 {
	var total int64
	for _, d := range b.Downloads {
		total += d.Bytes
	}
	return total
}

// RuntimeID is the on-disk bundle directory and the install progress key.
const RuntimeID = "rapidocr-json"

// Runtime is RapidOCR-json v0.9.0 (PP-OCRv5, zip). Official hiroi-sora
// builds are 7z; this zip is the one Go can unpack without an extra tool.
func Runtime() Bundle {
	const zipURL = "https://github.com/lsicbc/RapidOCR-json/releases/download/v0.9.0/RapidOCR-json_v0.9.0.zip"
	return Bundle{
		ID:     RuntimeID,
		Title:  "PP-OCR 本机识别引擎",
		Detail: "RapidOCR-json v0.9.0（Windows x64，约 28 MB）",
		Downloads: []Download{{
			Path: ".",
			URLs: []string{
				zipURL,
				"https://ghfast.top/" + zipURL,
			},
			SHA256:  "0f3e6ef3f1fa6029b230e8ae4a2ff58a5be73b25d4bf7a67eb799aa4463026be",
			Bytes:   29234358,
			Archive: ArchiveZip,
		}},
	}
}
