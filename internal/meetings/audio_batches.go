package meetings

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/oklog/ulid/v2"
)

// AudioBatchIdentity is stable across transport retries. Offsets count samples
// within this capture, not milliseconds rounded by the UI.
type AudioBatchIdentity struct {
	CaptureSessionID string `json:"captureSessionId"`
	ChunkSeq         int64  `json:"chunkSeq"`
	SampleStart      int64  `json:"sampleStart"`
	SampleCount      int64  `json:"sampleCount"`
	Digest           string `json:"digest"`
}

type AudioBatchAck struct {
	AudioBatchIdentity
	AudioMS int64 `json:"audioMs"`
}

// PCM and its ACK metadata commit together. No append to another audio file is
// needed, so death before rename leaves nothing committed; after rename a retry
// reads the same ACK. Temporary files are never part of the audio read view.
type audioBatchRecord struct {
	Version   int    `json:"version"`
	MeetingID string `json:"meetingId"`
	AudioBatchAck
	GlobalSampleStart int64  `json:"globalSampleStart"`
	PCM               []byte `json:"pcm"`
	StoredDigest      string `json:"storedDigest"`
}

var captureIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func batchName(id AudioBatchIdentity) string {
	return fmt.Sprintf("batch_%s_%012d.json", id.CaptureSessionID, id.ChunkSeq)
}

func validAudioBatch(id AudioBatchIdentity, pcm []byte) bool {
	return captureIDPattern.MatchString(id.CaptureSessionID) && id.ChunkSeq >= 0 && id.SampleStart >= 0 &&
		id.SampleCount > 0 && id.SampleCount <= 24576 && int64(len(pcm)) == id.SampleCount*2 &&
		digestPattern.MatchString(id.Digest) && pcmDigest(pcm) == id.Digest
}

func readAudioBatches(dir string) ([]audioBatchRecord, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "batch_*.json"))
	if err != nil {
		return nil, err
	}
	records := make([]audioBatchRecord, 0, len(paths))
	for _, path := range paths {
		var r audioBatchRecord
		if _, err := readMeetingJSON(path, &r); err != nil {
			return nil, err
		}
		if r.Version != 1 || r.MeetingID != filepath.Base(dir) || filepath.Base(path) != batchName(r.AudioBatchIdentity) ||
			!captureIDPattern.MatchString(r.CaptureSessionID) || r.ChunkSeq < 0 || r.SampleStart < 0 || r.SampleCount <= 0 || r.SampleCount > 24576 ||
			int64(len(r.PCM)) != r.SampleCount*2 || r.GlobalSampleStart < 0 || !digestPattern.MatchString(r.Digest) || pcmDigest(r.PCM) != r.StoredDigest ||
			r.AudioMS != pcmDurationMS((r.GlobalSampleStart+r.SampleCount)*2) {
			return nil, ErrConflict
		}
		r.PCM = nil // Index metadata only; long meetings must not retain all PCM.
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].GlobalSampleStart < records[j].GlobalSampleStart })
	cursor := legacyAudioBytes(dir) / 2
	captures := map[string]audioBatchRecord{}
	for _, r := range records {
		if r.GlobalSampleStart != cursor {
			return nil, ErrConflict
		}
		previous, exists := captures[r.CaptureSessionID]
		if (!exists && (r.ChunkSeq != 0 || r.SampleStart != 0)) || (exists && (r.ChunkSeq != previous.ChunkSeq+1 || r.SampleStart != previous.SampleStart+previous.SampleCount)) {
			return nil, ErrConflict
		}
		captures[r.CaptureSessionID] = r
		cursor += r.SampleCount
	}
	return records, nil
}

func (s *Service) AppendAudioBatch(ctx context.Context, meetingID string, pcm []byte, id AudioBatchIdentity) (AudioBatchAck, error) {
	if err := s.ready(); err != nil {
		return AudioBatchAck{}, err
	}
	if _, err := ulid.ParseStrict(meetingID); err != nil || !validAudioBatch(id, pcm) {
		return AudioBatchAck{}, ErrInvalid
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	m, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return AudioBatchAck{}, err
	}
	sink, err := s.ensureSink(meetingID)
	if err != nil {
		return AudioBatchAck{}, err
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if err := sink.loadBatchesLocked(); err != nil {
		return AudioBatchAck{}, err
	}
	if old, exists := sink.batches[batchName(id)]; exists {
		if old.AudioBatchIdentity != id {
			return AudioBatchAck{}, ErrConflict
		}
		return old.AudioBatchAck, nil // A committed ACK remains valid after Stop.
	}
	if m.Status != StatusRecording || sink.closed {
		return AudioBatchAck{}, ErrNotRecording
	}
	if mixed := s.takeMixPCM(meetingID, len(pcm)); len(mixed) > 0 {
		pcm = mixS16le(pcm, mixed)
	}
	return sink.appendBatchLocked(meetingID, pcm, id)
}

func (s *audioSink) loadBatchesLocked() error {
	if s.batches != nil {
		return nil
	}
	records, err := readAudioBatches(s.dir)
	if err != nil {
		return err
	}
	s.batches = map[string]audioBatchRecord{}
	s.captures = map[string]audioBatchRecord{}
	for _, r := range records {
		s.batches[batchName(r.AudioBatchIdentity)] = r
		s.captures[r.CaptureSessionID] = r
	}
	return nil
}

func (s *audioSink) appendBatchLocked(meetingID string, pcm []byte, id AudioBatchIdentity) (AudioBatchAck, error) {
	previous, exists := s.captures[id.CaptureSessionID]
	if (!exists && (id.ChunkSeq != 0 || id.SampleStart != 0)) || (exists && (id.ChunkSeq != previous.ChunkSeq+1 || id.SampleStart != previous.SampleStart+previous.SampleCount)) {
		return AudioBatchAck{}, ErrConflict
	}
	if s.file != nil {
		if err := finalizeWAV(s.file, s.chunkBytes); err != nil {
			return AudioBatchAck{}, err
		}
		if err := s.file.Sync(); err != nil {
			return AudioBatchAck{}, err
		}
		if err := s.file.Close(); err != nil {
			return AudioBatchAck{}, err
		}
		s.file = nil
	}
	r := audioBatchRecord{Version: 1, MeetingID: meetingID, AudioBatchAck: AudioBatchAck{AudioBatchIdentity: id, AudioMS: pcmDurationMS(s.totalBytes + int64(len(pcm)))}, GlobalSampleStart: s.totalBytes / 2, PCM: pcm, StoredDigest: pcmDigest(pcm)}
	if err := writeMeetingJSON(filepath.Join(s.dir, batchName(id)), r); err != nil {
		return AudioBatchAck{}, err
	}
	s.totalBytes += int64(len(pcm))
	r.PCM = nil
	s.batches[batchName(id)] = r
	s.captures[id.CaptureSessionID] = r
	return r.AudioBatchAck, nil
}
