import React from 'react'

export function ProjectHeroArt(): React.JSX.Element {
  return (
    <svg viewBox="40 16 560 248" fill="none" aria-hidden="true">
      <defs>
        <radialGradient id="proj-tide-core" cx="50%" cy="46%" r="50%">
          <stop offset="0%" stopColor="#d8ffe8" />
          <stop offset="28%" stopColor="#7CFF67" stopOpacity="0.85" />
          <stop offset="62%" stopColor="#3bd6ff" stopOpacity="0.35" />
          <stop offset="100%" stopColor="#7f5bff" stopOpacity="0" />
        </radialGradient>
        <linearGradient id="proj-tide-ring" x1="80" y1="40" x2="560" y2="240">
          <stop offset="0%" stopColor="#3bd6ff" />
          <stop offset="55%" stopColor="#7CFF67" />
          <stop offset="100%" stopColor="#7f5bff" />
        </linearGradient>
        <linearGradient id="proj-glass" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#ffffff" stopOpacity="0.16" />
          <stop offset="100%" stopColor="#ffffff" stopOpacity="0.04" />
        </linearGradient>
        <filter id="proj-soft" x="-30%" y="-30%" width="160%" height="160%">
          <feGaussianBlur stdDeviation="5" />
        </filter>
      </defs>
      <g fill="#b7c7de" opacity="0.55">
        <circle cx="86" cy="48" r="1.2" /><circle cx="140" cy="92" r="1" /><circle cx="210" cy="36" r="1.3" />
        <circle cx="520" cy="42" r="1.1" /><circle cx="588" cy="78" r="1.4" /><circle cx="70" cy="210" r="1.1" />
        <circle cx="600" cy="196" r="1.2" /><circle cx="430" cy="28" r="1" />
      </g>
      <ellipse cx="318" cy="156" rx="228" ry="86" stroke="url(#proj-tide-ring)" strokeOpacity="0.45" strokeWidth="1.25" />
      <ellipse cx="318" cy="156" rx="156" ry="54" stroke="#7CFF67" strokeOpacity="0.4" strokeWidth="1" />
      <ellipse cx="318" cy="150" rx="118" ry="78" fill="url(#proj-tide-core)" filter="url(#proj-soft)" />
      <path d="M168 118C210 96 250 132 318 148C390 166 430 112 492 128" stroke="#3bd6ff" strokeOpacity="0.75" strokeWidth="1.2" />
      <path d="M196 206C240 176 280 188 318 148C360 108 410 176 508 188" stroke="#7f5bff" strokeOpacity="0.7" strokeWidth="1.2" />
      <circle cx="168" cy="118" r="16" fill="url(#proj-glass)" stroke="#3bd6ff" strokeOpacity="0.8" />
      <path d="M160 122h16M168 114v16" stroke="#e8fff4" strokeWidth="1.2" strokeLinecap="round" />
      <circle cx="196" cy="206" r="13" fill="url(#proj-glass)" stroke="#7f5bff" strokeOpacity="0.85" />
      <circle cx="196" cy="206" r="3" fill="#7CFF67" />
      <circle cx="318" cy="148" r="22" fill="#0c1220" stroke="url(#proj-tide-ring)" strokeWidth="1.6" />
      <circle cx="318" cy="148" r="7" fill="#7CFF67" />
      <circle cx="492" cy="128" r="15" fill="url(#proj-glass)" stroke="#3bd6ff" strokeOpacity="0.8" />
      <path d="M485 132l5-8 5 8-5 3z" stroke="#e8fff4" strokeWidth="1.1" strokeLinejoin="round" />
      <circle cx="508" cy="188" r="14" fill="url(#proj-glass)" stroke="#7CFF67" strokeOpacity="0.75" />
      <path d="M501 188h14M508 181v14" stroke="#7CFF67" strokeWidth="1.1" strokeLinecap="round" />
      <rect x="448" y="36" width="148" height="72" rx="14" fill="url(#proj-glass)" stroke="#ffffff" strokeOpacity="0.18" />
      <path d="M466 88V62M486 88V52M506 88V70M526 88V46M546 88V66M566 88V58" stroke="url(#proj-tide-ring)" strokeWidth="3" strokeLinecap="round" />
      <rect x="64" y="156" width="132" height="64" rx="14" fill="url(#proj-glass)" stroke="#ffffff" strokeOpacity="0.16" />
      <path d="M78 196c12-16 18-6 28-18 10 14 16 4 26-10 8 12 14 2 22-8" stroke="#7CFF67" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="78" cy="172" r="3" fill="#3bd6ff" /><circle cx="168" cy="172" r="3" fill="#7f5bff" />
    </svg>
  )
}

export function AssetHeroArt(): React.JSX.Element {
  return (
    <svg viewBox="-8 -16 540 246" fill="none" aria-hidden="true">
      <defs>
        <radialGradient id="asset-vault" cx="62%" cy="48%" r="48%">
          <stop offset="0%" stopColor="#d7fff0" />
          <stop offset="18%" stopColor="#7CFF67" stopOpacity="0.75" />
          <stop offset="48%" stopColor="#3bd6ff" stopOpacity="0.35" />
          <stop offset="100%" stopColor="#7f5bff" stopOpacity="0" />
        </radialGradient>
        <linearGradient id="asset-sheet-a" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#243044" />
          <stop offset="100%" stopColor="#121826" />
        </linearGradient>
        <linearGradient id="asset-sheet-b" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor="#1c3350" />
          <stop offset="100%" stopColor="#101820" />
        </linearGradient>
      </defs>
      <ellipse cx="300" cy="112" rx="150" ry="78" fill="url(#asset-vault)" />
      <g transform="translate(78 46) rotate(-16 90 60)">
        <rect width="200" height="128" rx="18" fill="url(#asset-sheet-a)" stroke="#3bd6ff" strokeWidth="1.4" />
        <path d="M24 36h90M24 56h130M24 76h72" stroke="#8fdfff" strokeWidth="3" strokeLinecap="round" />
      </g>
      <g transform="translate(168 34) rotate(7 100 64)">
        <rect width="220" height="136" rx="18" fill="url(#asset-sheet-b)" stroke="#7fb4ff" strokeWidth="1.6" />
        <path d="M26 40h110M26 62h148M26 84h80" stroke="#d7ecff" strokeWidth="3" strokeLinecap="round" />
        <rect x="156" y="96" width="40" height="20" rx="6" fill="#7CFF67" />
      </g>
      <g transform="translate(250 58)">
        <path d="M48 8 92 32v48L48 104 4 80V32Z" fill="#0e1624" stroke="#7CFF67" strokeWidth="1.8" />
        <circle cx="48" cy="56" r="8" fill="#7CFF67" />
      </g>
      <rect x="24" y="150" width="132" height="48" rx="14" fill="#152033" stroke="#3bd6ff" strokeWidth="1.3" />
      <path d="M40 174h52M100 174h36" stroke="#3bd6ff" strokeWidth="3" strokeLinecap="round" />
      <rect x="360" y="148" width="128" height="48" rx="14" fill="#1a1630" stroke="#a78bfa" strokeWidth="1.3" />
      <path d="M376 172h48M432 172h36" stroke="#c4b5fd" strokeWidth="3" strokeLinecap="round" />
    </svg>
  )
}
