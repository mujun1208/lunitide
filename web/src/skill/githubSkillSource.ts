const COMMIT_SHA = /^[0-9a-f]{40}$/i

export type GithubSkillRef = {
  owner: string
  repo: string
  commit: string
  directory: string
  ref: string
}

export function canonicalizeGithubUrl(raw: string): string {
  let value = raw.trim().replace(/^[<'"]+|[>'"]+$/g, '')
  if (/^git@github\.com:/i.test(value)) value = `https://github.com/${value.slice(value.indexOf(':') + 1)}`
  value = value.replace(/^(https?:\/\/)?(www\.)?github\.com\//i, 'https://github.com/')
  try {
    const parsed = new URL(value)
    if (parsed.hostname.toLowerCase() !== 'github.com') return ''
    return `https://github.com${parsed.pathname}`.replace(/\/+$/, '')
  } catch {
    return ''
  }
}

export function parseGithubSkillSource(raw: string): GithubSkillRef | null {
  const canonical = canonicalizeGithubUrl(raw)
  const match = canonical.match(/^https:\/\/github\.com\/([^/]+)\/([^/]+?)(?:\.git)?(?:\/(?:tree|blob)\/([^/]+)(?:\/(.*))?)?$/i)
  if (!match) return null
  const owner = match[1], repo = match[2], gitRef = match[3] || 'HEAD'
  let directory = (match[4] || '').replace(/\/+$/, '')
  if (/\/skill\.md$/i.test(directory)) directory = directory.replace(/\/skill\.md$/i, '')
  else if (/^skill\.md$/i.test(directory)) directory = ''
  const commit = COMMIT_SHA.test(gitRef) ? gitRef.toLowerCase() : ''
  return { owner, repo, commit, directory, ref: commit || gitRef }
}

export function pinGithubSkillUrl(owner: string, repo: string, sha: string, directory = ''): string {
  const base = `https://github.com/${owner}/${repo}`
  const dir = directory.replace(/^\/+|\/+$/g, '')
  return dir ? `${base}/tree/${sha}/${dir}` : base
}

export function normalizeCommitSha(value: string): string | null {
  const sha = value.trim().toLowerCase()
  if (!sha) return ''
  return COMMIT_SHA.test(sha) ? sha : null
}

export async function resolveGithubCommit(
  owner: string,
  repo: string,
  gitRef = 'HEAD',
  fetchImpl: typeof fetch = fetch,
): Promise<string> {
  const response = await fetchImpl(`https://api.github.com/repos/${owner}/${repo}/commits/${encodeURIComponent(gitRef)}`, {
    headers: { Accept: 'application/vnd.github+json' },
  })
  if (!response.ok) throw new Error('无法读取仓库当前提交，请确认仓库公开后重试。')
  const body = await response.json() as { sha?: string }
  const sha = normalizeCommitSha(body.sha || '')
  if (!sha) throw new Error('无法读取仓库当前提交，请确认仓库公开后重试。')
  return sha
}

export function skillSourceDirectory(contentsUrl: string, slug: string, path = ''): string {
  if (path) return path.replace(/^\/+|\/+$/g, '')
  const marker = '/contents/'
  const index = contentsUrl.indexOf(marker)
  const prefix = index >= 0 ? contentsUrl.slice(index + marker.length).replace(/\/+$/, '') : ''
  return prefix ? `${prefix}/${slug}` : slug
}
