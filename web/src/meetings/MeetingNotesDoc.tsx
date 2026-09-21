import React from 'react'
import { MermaidBlock } from '../session/markdown/MermaidBlock'
import { attendeeInitial, attendeeTone, parseMeetingNotesDoc, type NotesAction, type NotesSection } from './notesDoc'

function formatWhen(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function SectionCard({ section, deferDiagrams }: { section: NotesSection; deferDiagrams?: boolean }) {
  return (
    <section className={`notes-doc-card notes-doc-${section.kind}`}>
      {section.kind === 'summary' && section.heading === '会议摘要' ? null : <h3>{section.heading}</h3>}
      {section.paragraphs.map(text => <p key={text.slice(0, 48)}>{text}</p>)}
      {section.bullets.length > 0 ? (
        <ul>{section.bullets.map(item => <li key={item}>{item}</li>)}</ul>
      ) : null}
      {section.reasoning.length > 0 ? (
        <div className="notes-doc-reasoning" aria-label="思考记录">
          <p className="notes-doc-subhead">思考</p>
          <ul>{section.reasoning.map(item => <li key={item}>{item}</li>)}</ul>
        </div>
      ) : null}
      {section.table ? (
        <div className="notes-doc-table-wrap">
          {section.table.caption ? <p className="notes-doc-table-caption">{section.table.caption}</p> : null}
          <table className="notes-doc-table">
            <thead>
              <tr>{section.table.headers.map(header => <th key={header}>{header}</th>)}</tr>
            </thead>
            <tbody>
              {section.table.rows.map((row, index) => (
                <tr key={`${index}:${row.join('|')}`}>
                  {row.map((cell, cellIndex) => <td key={`${cellIndex}:${cell}`}>{cell}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {section.diagram ? (
        <div className="notes-doc-diagram" aria-label="流程图">
          {section.diagram.caption ? <p className="notes-doc-subhead">{section.diagram.caption}</p> : null}
          {/* While the notes are still streaming, the document re-renders every
              publish. Mermaid loads its engine and lays out under one global
              lock, so drawing mid-stream competes with the live transcript for
              the main thread. Hold the flow until the document settles. */}
          <MermaidBlock source={section.diagram.code} wait={deferDiagrams} />
        </div>
      ) : null}
    </section>
  )
}

function ActionRow({ item }: { item: NotesAction }) {
  return (
    <li>
      <span className="notes-doc-mark" aria-hidden="true" />
      <div>
        <b>{item.task}</b>
        {item.owner || item.due ? (
          <small>{[item.owner, item.due ? `截止 ${item.due}` : ''].filter(Boolean).join(' · ')}</small>
        ) : null}
      </div>
    </li>
  )
}

export function MeetingNotesDoc({
  startedAt,
  durationLabel,
  summary,
  actions,
  emptySummaryHint,
  emptyActionsHint,
  deferDiagrams,
}: {
  startedAt?: string
  durationLabel?: string
  summary: string
  actions: string
  emptySummaryHint: string
  emptyActionsHint: string
  deferDiagrams?: boolean
}): React.JSX.Element {
  const doc = parseMeetingNotesDoc(summary, actions)
  const meta = [formatWhen(startedAt || ''), durationLabel].filter(Boolean).join(' · ')
  return (
    <div className="notes-doc" aria-label="会议纪要文档">
      {doc.attendees.length > 0 ? (
        <div className="notes-doc-people" aria-label="参会人">
          {doc.attendees.map(name => (
            <span className="notes-doc-chip" key={name}>
              <span className="notes-doc-avatar" style={{ background: attendeeTone(name) }}>{attendeeInitial(name)}</span>
              {name}
            </span>
          ))}
        </div>
      ) : null}
      {meta ? <p className="notes-doc-meta">{meta}</p> : null}
      {doc.sections.length === 0 ? (
        <section className="notes-doc-card notes-doc-empty"><p>{emptySummaryHint}</p></section>
      ) : doc.sections.map(section => (
        <SectionCard key={`${section.kind}:${section.heading}`} section={section} deferDiagrams={deferDiagrams} />
      ))}
      <section className="notes-doc-card notes-doc-todos">
        <h3>待办</h3>
        {doc.actions.length === 0 ? (
          <p className="notes-doc-empty-line">{emptyActionsHint}</p>
        ) : (
          <ul className="notes-doc-todo">
            {doc.actions.map(item => <ActionRow key={`${item.owner}:${item.task}:${item.due}`} item={item} />)}
          </ul>
        )}
      </section>
    </div>
  )
}
