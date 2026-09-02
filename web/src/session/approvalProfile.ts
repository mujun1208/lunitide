export function isDangerousCompanionTool(name: string): boolean {
  const n = name.trim()
  if (!n) return false
  if (n.startsWith('cc.') || n.startsWith('desktop.')) return true
  return n === 'command.run' || n === 'run_terminal_cmd' || n === 'im.send' || n === 'computer.act'
}

export function isFullDiskCompanionWrite(name: string): boolean {
  const n = name.trim()
  return n === 'workspace.write' || n === 'workspace.edit'
}

export function companionMayAutoApprove(name: string, fullDisk?: boolean): boolean {
  if (name === 'user.ask' || isDangerousCompanionTool(name)) return false
  if (isFullDiskCompanionWrite(name) && fullDisk !== false) return false
  return true
}
