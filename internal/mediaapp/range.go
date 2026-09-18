package mediaapp

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const MaxRangeBytes = 256 * 1024

var (
	ErrRangeInvalid       = errors.New("MEDIA_RANGE_INVALID")
	ErrRangeUnsatisfiable = errors.New("MEDIA_RANGE_UNSATISFIABLE")
	ErrRangeMulti         = errors.New("MEDIA_RANGE_MULTI")
)

type RangeResult struct {
	Status        int
	ContentType   string
	AcceptRanges  string
	ContentRange  string
	ContentLength int64
	Body          []byte
}

// ServeFileRange answers a single-byte Range against a local file.
// Multi-range requests fail closed. A satisfiable range is capped at MaxRangeBytes.
func ServeFileRange(path, rangeHeader, contentType string) (RangeResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return RangeResult{}, err
	}
	size := info.Size()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if strings.TrimSpace(rangeHeader) == "" {
		if size > MaxRangeBytes {
			return ServeFileRange(path, "bytes=0-"+strconv.FormatInt(MaxRangeBytes-1, 10), contentType)
		}
		body, err := readAt(path, 0, size)
		if err != nil {
			return RangeResult{}, err
		}
		return RangeResult{
			Status:        http.StatusOK,
			ContentType:   contentType,
			AcceptRanges:  "bytes",
			ContentLength: int64(len(body)),
			Body:          body,
		}, nil
	}
	start, end, err := parseSingleByteRange(rangeHeader, size)
	if err != nil {
		return RangeResult{}, err
	}
	length := end - start + 1
	if length > MaxRangeBytes {
		length = MaxRangeBytes
	}
	body, err := readAt(path, start, length)
	if err != nil {
		return RangeResult{}, err
	}
	return RangeResult{
		Status:        http.StatusPartialContent,
		ContentType:   contentType,
		AcceptRanges:  "bytes",
		ContentRange:  "bytes " + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(start+int64(len(body))-1, 10) + "/" + strconv.FormatInt(size, 10),
		ContentLength: int64(len(body)),
		Body:          body,
	}, nil
}

func parseSingleByteRange(header string, size int64) (start, end int64, err error) {
	raw := strings.TrimSpace(header)
	if strings.Contains(raw, ",") {
		return 0, 0, ErrRangeMulti
	}
	if !strings.HasPrefix(strings.ToLower(raw), "bytes=") {
		return 0, 0, ErrRangeInvalid
	}
	spec := strings.TrimSpace(raw[len("bytes="):])
	if spec == "" || strings.Count(spec, "-") != 1 {
		return 0, 0, ErrRangeInvalid
	}
	left, right, _ := strings.Cut(spec, "-")
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	if left == "" && right == "" {
		return 0, 0, ErrRangeInvalid
	}
	if size <= 0 {
		return 0, 0, ErrRangeUnsatisfiable
	}
	if left == "" {
		suffix, convErr := strconv.ParseInt(right, 10, 64)
		if convErr != nil || suffix <= 0 {
			return 0, 0, ErrRangeInvalid
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, nil
	}
	start, convErr := strconv.ParseInt(left, 10, 64)
	if convErr != nil || start < 0 {
		return 0, 0, ErrRangeInvalid
	}
	if start >= size {
		return 0, 0, ErrRangeUnsatisfiable
	}
	if right == "" {
		return start, size - 1, nil
	}
	end, convErr = strconv.ParseInt(right, 10, 64)
	if convErr != nil || end < start {
		return 0, 0, ErrRangeInvalid
	}
	if end >= size {
		end = size - 1
	}
	return start, end, nil
}

func readAt(path string, offset, length int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, length)
	n, err := io.ReadFull(f, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return buf[:n], nil
	}
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func ServeMediaHTTP(path, method, rangeHeader, contentType string) (RangeResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return RangeResult{}, err
	}
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "HEAD":
	default:
		return RangeResult{Status: http.StatusMethodNotAllowed, ContentType: contentType}, nil
	}
	result, err := ServeFileRange(path, rangeHeader, contentType)
	if err != nil {
		if errors.Is(err, ErrRangeMulti) || errors.Is(err, ErrRangeInvalid) || errors.Is(err, ErrRangeUnsatisfiable) {
			return RangeResult{
				Status:        http.StatusRequestedRangeNotSatisfiable,
				ContentType:   contentType,
				AcceptRanges:  "bytes",
				ContentRange:  "bytes */" + strconv.FormatInt(info.Size(), 10),
				ContentLength: 0,
			}, nil
		}
		return RangeResult{}, err
	}
	if strings.EqualFold(method, "HEAD") {
		result.Body = nil
	}
	return result, nil
}

func (s *Service) ServeTicketRange(token, owner, rangeHeader, contentType string) (RangeResult, error) {
	path, mime, err := s.ResolveTicket(token, owner)
	if err != nil {
		return RangeResult{}, err
	}
	if contentType == "" {
		contentType = mime
	}
	return ServeMediaHTTP(path, http.MethodGet, rangeHeader, contentType)
}
