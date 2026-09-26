/** Selected text from the side browser, placed in the chat composer as a quote. */
export function composerWithPreviewQuote(prev: string, quote: string): string {
  const text = quote.trim()
  if (!text) return prev
  const block = text.split('\n').map(line => `> ${line}`).join('\n')
  const base = prev.replace(/\s+$/, '')
  return base ? `${base}\n\n${block}\n` : `${block}\n`
}
