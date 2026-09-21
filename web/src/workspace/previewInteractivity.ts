/** An HTML artifact is previewed with scripts disabled, so a generated page that
 *  navigates through menus or reads its own saved data renders once and then does
 *  nothing when clicked. That is deliberate — see isolatedHTML — but silence about
 *  it reads as a broken preview. Detect the documents where it will be noticed so
 *  the surface can say so and offer the browser instead. */
export function previewNeedsScripts(content: string): boolean {
  if (!content) return false
  // A <script> tag, an inline handler, or a form: each one is behaviour the
  // reader will try and find missing. Anything else previews faithfully.
  return /<script[\s>]/i.test(content) || /\son(?:click|change|submit|input|load)\s*=/i.test(content) || /<form[\s>]/i.test(content)
}
