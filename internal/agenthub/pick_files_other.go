//go:build !windows

package agenthub

func pickFilesOS() ([]string, error) { return nil, ErrPickCanceled }
func pickFolderOS() (string, error)  { return "", ErrPickCanceled }
