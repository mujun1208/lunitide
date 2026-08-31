package main

// engineWatchShouldRelaunch is D11 after G1: killing the tray/window must
// leave a live engine alone. Only a dead engine (RPC poison or PID gone)
// makes the desktop/tray spawn a replacement.
func engineWatchShouldRelaunch(shuttingDown, rpcBroken bool, pid int, pidAlive bool) bool {
	if shuttingDown {
		return false
	}
	if rpcBroken {
		return true
	}
	if pid > 0 && !pidAlive {
		return true
	}
	return false
}
