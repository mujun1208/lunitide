package app

import "sync/atomic"

// catalogProbeSuppressShell stops a fresh diagnostic check from handing a
// temporary file to the system editor. The check deletes that file when it
// finishes; Notepad would then ask to create it.
var catalogProbeSuppressShell atomic.Bool

var openArtifactShell = openArtifactShellImpl

func openLocalArtifactPath(path string) error {
	if catalogProbeSuppressShell.Load() {
		return nil
	}
	return openArtifactShell(path)
}
