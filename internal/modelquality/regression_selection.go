package modelquality

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	ErrEmptyRegressionSelection = errors.New("regression selection is empty")
	ErrZeroRegressionMatches    = errors.New("regression selection matched no tests")
)

func SelectRegressionTests(names []string, pattern string) ([]string, error) {
	if pattern == "" {
		return nil, ErrEmptyRegressionSelection
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("regression selection: %w", err)
	}
	var matched []string
	for _, name := range names {
		if re.MatchString(name) {
			matched = append(matched, name)
		}
	}
	if len(matched) == 0 {
		return nil, ErrZeroRegressionMatches
	}
	return matched, nil
}
