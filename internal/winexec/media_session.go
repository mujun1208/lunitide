package winexec

type MediaSessionResult struct {
	App      string `json:"app"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Verified bool   `json:"verified"`
	Shuffle  bool   `json:"shuffle"`
}
