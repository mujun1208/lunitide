export function workspaceDownloadIsText(mime = '', name = ''): boolean {
  if (/^(text\/|application\/(json|xml|javascript|x-yaml|yaml))/i.test(mime)) return true
  return /\.(txt|md|markdown|csv|json|xml|html|htm|css|js|ts|tsx|jsx|go|py|rs|yml|yaml|toml|ini|log)$/i.test(name)
}

export function workspaceDownloadEnabled(input: {
  name: string
  mime?: string
  contentBase64?: string
  parsedText?: string
  localText?: string
}): boolean {
  if (input.contentBase64) return true
  if (!workspaceDownloadIsText(input.mime, input.name)) return false
  return input.parsedText !== undefined || input.localText !== undefined
}
