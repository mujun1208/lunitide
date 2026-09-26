package producthub

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/tts"
	"github.com/lunitide/lunitide/internal/voice"
)

// DefaultLiveRun is what 「重新检测」 executes: dictation, playback, the Kokoro
// size check, image OCR, and the newest engine log. It does not install
// anything and does not edit product code.
func DefaultLiveRun(ctx context.Context) ([]TaskResult, string) {
	return []TaskResult{
		probeDictate(ctx),
		probePlay(ctx),
		probeDownload(ctx),
		probeOCR(ctx),
	}, readEngineLog()
}

func probeDictate(ctx context.Context) TaskResult {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	started := time.Now()
	refiner := &voice.Refiner{Root: tts.RefEngineDataRoot(), Startup: 20 * time.Second, Budget: 22 * time.Second}
	defer refiner.Shutdown()
	if err := refiner.Ready(ctx); err != nil {
		return TaskResult{ID: "dictate", Title: "听写", Status: "untested", Evidence: "听写模型未就绪，没有开始识别：" + err.Error()}
	}
	pcm := make([]byte, voice.SampleRate*voice.BytesPerSample*4/10)
	text, err := refiner.Transcribe(ctx, pcm)
	elapsed := time.Since(started).Round(time.Millisecond)
	if err != nil {
		return TaskResult{ID: "dictate", Title: "听写", Status: "fail", Evidence: fmt.Sprintf("耗时 %s：%v", elapsed, err)}
	}
	return TaskResult{ID: "dictate", Title: "听写", Status: "pass", Evidence: fmt.Sprintf("识别已跑完，耗时 %s，文本 %q", elapsed, text)}
}

func probePlay(ctx context.Context) TaskResult {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	started := time.Now()
	path := filepath.Join(os.TempDir(), "lunitide-probe.wav")
	if err := os.WriteFile(path, silentWAV(3200), 0o600); err != nil {
		return TaskResult{ID: "play", Title: "播放", Status: "untested", Evidence: "写探测音失败：" + err.Error()}
	}
	script := fmt.Sprintf("$p = New-Object System.Media.SoundPlayer '%s'; $p.PlaySync()", strings.ReplaceAll(path, "'", "''"))
	if err := runPS(ctx, script); err != nil {
		return TaskResult{ID: "play", Title: "播放", Status: "fail", Evidence: fmt.Sprintf("耗时 %s：%v", time.Since(started).Round(time.Millisecond), err)}
	}
	return TaskResult{ID: "play", Title: "播放", Status: "pass", Evidence: fmt.Sprintf("已播放 0.2 秒探测音，耗时 %s", time.Since(started).Round(time.Millisecond))}
}

func probeDownload(ctx context.Context) TaskResult {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	started := time.Now()
	elapsed := func() time.Duration { return time.Since(started).Round(time.Millisecond) }
	model := tts.OnnxModelBundle()
	if len(model.Downloads) == 0 || len(model.Downloads[0].URLs) == 0 {
		return TaskResult{ID: "download", Title: "下载", Status: "untested", Evidence: fmt.Sprintf("耗时 %s。没有 Kokoro 模型地址", elapsed())}
	}
	pin := model.Downloads[0].Bytes
	root := tts.RefEngineDataRoot()
	inst := &voice.Installer{Root: root}
	local := inst.Installed(tts.OnnxRuntimeBundle()) && inst.Installed(model)
	offered, err := offeredLength(ctx, model.Downloads[0].URLs[0])
	if err != nil {
		state := "本机模型目录已核对"
		status := "untested"
		if !local {
			state = "本机模型未装好"
			status = "fail"
		}
		return TaskResult{ID: "download", Title: "下载", Status: status, Evidence: fmt.Sprintf("耗时 %s。%s。这次没有在时限内拿到服务器文件长度：%v", elapsed(), state, err)}
	}
	if offered <= 0 {
		return TaskResult{ID: "download", Title: "下载", Status: "untested", Evidence: fmt.Sprintf("耗时 %s。服务器没有给出文件长度", elapsed())}
	}
	evidence := fmt.Sprintf("耗时 %s。记录 %d 字节，服务器 %d 字节，本机模型目录 %s", elapsed(), pin, offered, map[bool]string{true: "已核对", false: "未装好"}[local])
	if offered != pin || !local {
		return TaskResult{ID: "download", Title: "下载", Status: "fail", Evidence: evidence}
	}
	return TaskResult{ID: "download", Title: "下载", Status: "pass", Evidence: evidence}
}

func probeOCR(ctx context.Context) TaskResult {
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	started := time.Now()
	path := filepath.Join(os.TempDir(), "lunitide-probe-ocr.png")
	script := fmt.Sprintf(`Add-Type -AssemblyName System.Drawing
$bmp = New-Object System.Drawing.Bitmap 320, 96
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.Clear([System.Drawing.Color]::White)
$font = New-Object System.Drawing.Font 'Arial', 42
$g.DrawString('OCR', $font, [System.Drawing.Brushes]::Black, 24, 16)
$bmp.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)
$g.Dispose(); $bmp.Dispose()`, strings.ReplaceAll(path, "'", "''"))
	if err := runPS(ctx, script); err != nil {
		return TaskResult{ID: "ocr", Title: "图片识别", Status: "untested", Evidence: "没有生成探测图：" + err.Error()}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return TaskResult{ID: "ocr", Title: "图片识别", Status: "untested", Evidence: err.Error()}
	}
	got, err := doctext.ExtractImageOCR(ctx, raw)
	elapsed := time.Since(started).Round(time.Millisecond)
	if err != nil {
		return TaskResult{ID: "ocr", Title: "图片识别", Status: "fail", Evidence: fmt.Sprintf("耗时 %s：%v", elapsed, err)}
	}
	text := ""
	if len(got.Pages) > 0 {
		text = strings.TrimSpace(got.Pages[0].Text)
	}
	if !strings.Contains(strings.ToUpper(text), "OCR") {
		return TaskResult{ID: "ocr", Title: "图片识别", Status: "fail", Evidence: fmt.Sprintf("耗时 %s，读出 %q", elapsed, text)}
	}
	return TaskResult{ID: "ocr", Title: "图片识别", Status: "pass", Evidence: fmt.Sprintf("耗时 %s，读出 %q", elapsed, text)}
}

func offeredLength(ctx context.Context, rawURL string) (int64, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if n := lengthFromRange(resp.Header.Get("Content-Range")); n > 0 {
		return n, nil
	}
	if resp.ContentLength > 0 {
		return resp.ContentLength, nil
	}
	return 0, fmt.Errorf("HTTP %d，没有文件长度", resp.StatusCode)
}

func lengthFromRange(header string) int64 {
	_, total, ok := strings.Cut(header, "/")
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func readEngineLog() string {
	dir := filepath.Join(tts.RefEngineDataRoot(), "logs")
	matches, err := filepath.Glob(filepath.Join(dir, "engine-*.log"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	if len(matches) > 3 {
		matches = matches[len(matches)-3:]
	}
	var b strings.Builder
	for _, name := range matches {
		buf, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		if len(buf) > 512<<10 {
			buf = buf[len(buf)-512<<10:]
		}
		b.Write(buf)
		b.WriteByte('\n')
	}
	return b.String()
}

func runPS(ctx context.Context, script string) error {
	exe := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 240 {
			msg = msg[len(msg)-240:]
		}
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func silentWAV(samples int) []byte {
	dataLen := samples * 2
	buf := make([]byte, 44+dataLen)
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataLen))
	copy(buf[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], 16000)
	binary.LittleEndian.PutUint32(buf[28:], 32000)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataLen))
	return buf
}
