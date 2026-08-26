package ccapp

// bgraHasVisiblePixels reports whether a 32-bit BGRA buffer contains any
// non-black sample. Failed GDI captures often return success with an
// all-zero DIB (the last Windows smoke failure).
func bgraHasVisiblePixels(pixels []byte) bool {
	n := len(pixels)
	if n < 4 {
		return false
	}
	step := 64
	if n < 1024 {
		step = 4
	}
	for i := 0; i+2 < n; i += step {
		if pixels[i] != 0 || pixels[i+1] != 0 || pixels[i+2] != 0 {
			return true
		}
	}
	for _, off := range []int{0, (n / 2) &^ 3, n - 4} {
		if off >= 0 && off+2 < n && (pixels[off] != 0 || pixels[off+1] != 0 || pixels[off+2] != 0) {
			return true
		}
	}
	return false
}
