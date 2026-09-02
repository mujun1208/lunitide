package toolruntime

import (
	"encoding/json"

	"github.com/lunitide/lunitide/internal/ccapp"
)

// officeGenTools are the P2-1 generators: they mutate the session
// workspace, so they ride the workspace.write approval class.
var officeGenTools = map[string]bool{
	"excel.gen": true, "docx.gen": true, "pptx.gen": true, "pdf.gen": true, "html.gen": true,
}

// ccToolChangesMachine folds computer control into the mutating gate. The
// wrapper tools (desktop.type, media.play) were already listed, but a direct
// cc.* or computer.act call reached the desktop through ccapp alone, and ccapp
// only pauses for high/critical risk — a click is medium.
func ccToolChangesMachine(name string, args json.RawMessage) bool {
	if name == ccapp.ToolComputerAct {
		mapped, _, err := ccapp.MapComputerAct(args)
		if err != nil {
			// Fail closed: ccapp will refuse an unmappable payload anyway, and
			// assuming "harmless" is the wrong way to be wrong here.
			return true
		}
		return ccapp.ToolChangesMachine(mapped)
	}
	return ccapp.ToolChangesMachine(name)
}
