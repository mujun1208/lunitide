package mediajob

import "os"

type Journal struct {
	File   string
	Total  int
	Done   []int
	Cancel bool
}

func ExportTTSFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}

func Remaining(j Journal) []int {
	if j.Cancel {
		return nil
	}
	seen := map[int]bool{}
	for _, d := range j.Done {
		seen[d] = true
	}
	var out []int
	for i := 0; i < j.Total; i++ {
		if !seen[i] {
			out = append(out, i)
		}
	}
	return out
}
