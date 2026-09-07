/** Only use for engine-owned tool rows, never for user or assistant prose. */
export function historicalToolText(text: string): string {
  return text.replace(/^\[tool-result [^\]\r\n]*\](?:\r?\n|$)/, '').trim() || '工具执行完毕'
}
