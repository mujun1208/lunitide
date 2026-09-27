package people

import "sync/atomic"

// suppressShell keeps a diagnostic probe from handing a temporary file to the
// system editor. The probe deletes that file when it finishes.
var suppressShell atomic.Bool

func SuppressShell(on bool) { suppressShell.Store(on) }
