/** Preview documents without scripts, network access, nested frames or forms. */
export const isolatedHTML = (content: string) =>
  `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: blob:; style-src 'unsafe-inline'; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">${content}`

/** A generated page with no preview ticket still runs here. Scripts and buttons
 *  work. The network, nested frames, and this application's origin stay closed. */
export const runnableHTML = (content: string) =>
  `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'">${content}`
