package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lunitide/lunitide/internal/modelquality"
)

func main() {
	pattern := flag.String("pattern", "", "regexp of test names")
	namesFile := flag.String("names-file", "", "file of test names, one per line")
	flag.Parse()

	if *pattern == "" {
		fmt.Fprintln(os.Stderr, modelquality.ErrEmptyRegressionSelection)
		os.Exit(2)
	}

	names, err := readNames(*namesFile, os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	selected, err := modelquality.SelectRegressionTests(names, *pattern)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if err == modelquality.ErrEmptyRegressionSelection {
			os.Exit(2)
		}
		os.Exit(1)
	}
	for _, name := range selected {
		fmt.Println(name)
	}
}

func readNames(path string, stdin io.Reader) ([]string, error) {
	src := stdin
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		src = f
	}
	var names []string
	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name != "" {
			names = append(names, name)
		}
	}
	return names, scanner.Err()
}
