import React from 'react'

const MARKS: Record<string, React.ReactNode> = {
  new: <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M8 3.2v9.6M3.2 8h9.6" />,
  search: (
    <>
      <circle cx="7" cy="7" r="4.1" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="m10.2 10.2 3 3" />
    </>
  ),
  settings: (
    <>
      <circle cx="8" cy="8" r="2.1" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.4 3.4l1.1 1.1M11.5 11.5l1.1 1.1M12.6 3.4l-1.1 1.1M4.5 11.5l-1.1 1.1" />
    </>
  ),
  office: (
    <>
      <rect x="2.2" y="2.2" width="5" height="5" rx="1" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <rect x="8.8" y="2.2" width="5" height="5" rx="1" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <rect x="2.2" y="8.8" width="5" height="5" rx="1" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <rect x="8.8" y="8.8" width="5" height="5" rx="1" fill="none" stroke="currentColor" strokeWidth="1.4" />
    </>
  ),
  automation: (
    <>
      <circle cx="8" cy="8" r="5.2" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M8 5.2V8l2 1.4" />
    </>
  ),
  media: <path fill="currentColor" d="M6.2 4.4v7.2L12 8Z" />,
  people: (
    <>
      <circle cx="6" cy="6" r="2" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <circle cx="11" cy="6.5" r="1.6" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M2.4 12.2c.5-1.8 2-2.7 3.6-2.7s3.1.9 3.6 2.7M10 9.6c1.2 0 2.3.6 2.8 1.8" />
    </>
  ),
  mro: (
    <>
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M3.2 10.2 5.8 7.6l2.6 2.6" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="m6.4 6.8 1.2-1.2 3.6 3.6-1.2 1.2" />
      <circle cx="11.6" cy="4.4" r="1.3" fill="none" stroke="currentColor" strokeWidth="1.4" />
    </>
  ),
  meetings: (
    <>
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" d="M3.2 2.6h9.6v10.8H3.2Z" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M5.2 6h5.6M5.2 8.4h5.6M5.2 10.8h3.4" />
    </>
  ),
  productHub: <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" d="M8 2.2 13 5v6L8 13.8 3 11V5Z" />,
  projects: (
    <>
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" d="M2.2 5.2h11.6v7.2H2.2Z" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" d="M2.2 5.2 3.6 3.2h3.2l1 2" />
    </>
  ),
  skill: <path fill="currentColor" d="M8 1.6 9.1 6 13.6 6.2 10.1 8.8 11.4 13.2 8 10.8 4.6 13.2 5.9 8.8 2.4 6.2 6.9 6Z" />,
  expert: (
    <>
      <circle cx="8" cy="5.4" r="2.2" fill="none" stroke="currentColor" strokeWidth="1.4" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M3.2 13c.7-2.2 2.4-3.2 4.8-3.2s4.1 1 4.8 3.2" />
    </>
  ),
  mcp: (
    <>
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M6 6.2 9.8 2.4a2 2 0 0 1 2.8 2.8L8.8 9" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="M10 9.8 6.2 13.6a2 2 0 0 1-2.8-2.8L7.2 7" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" d="m6.2 9.8 3.6-3.6" />
    </>
  ),
  plugins: <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" d="M6.2 2.4h3.6V5H13v3.2H9.8v2.2H6.2V8.2H3V5h3.2Z" />,
  assets: (
    <>
      <path fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" d="M2.4 5.2 8 2.4l5.6 2.8v5.6L8 13.6 2.4 10.8Z" />
      <path fill="none" stroke="currentColor" strokeWidth="1.4" d="M8 8v5.6M2.4 5.2 8 8l5.6-2.8" />
    </>
  ),
}

export function NavIcon({ name }: { name: string }): React.JSX.Element {
  return (
    <svg className="nav-ico" viewBox="0 0 16 16" aria-hidden="true">
      {MARKS[name] ?? null}
    </svg>
  )
}
