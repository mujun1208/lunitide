import React, { useEffect, useRef } from 'react'
import { readAllMroPages } from '../mro/pagination'
import {
  createMutationAttempt,
  datasourceBridge,
  type ExpertBridge,
  type MroBridge,
  type ProjectBridge,
  type SessionBridge,
} from '../bridge/client'
import { openMroChat } from '../mro/MroAskButton'
import { MroWorkbenchPage } from '../mro/MroWorkbenchPage'
import type { WorkbenchRail } from '../expert/expertIds'
import { ensurePersonalProject } from './appHelpers'
import type { LaunchTarget } from './appTypes'

export function MroWorkbenchRoute({
  mro,
  experts,
  projects,
  sessions,
  mroEnabled,
  mroExpertId,
  opsExpertIds,
  mroInitialRail,
  setDraftSessionIds,
  setTarget,
}: {
  mro: MroBridge
  experts: ExpertBridge
  projects: ProjectBridge
  sessions: SessionBridge
  mroEnabled: boolean
  mroExpertId: string
  opsExpertIds: Record<string, string>
  mroInitialRail: WorkbenchRail
  setDraftSessionIds: React.Dispatch<React.SetStateAction<Set<string>>>
  setTarget: (target: LaunchTarget | undefined) => void
}): React.JSX.Element {
  const alive = useRef(false)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  return (
    <MroWorkbenchPage
      enabled={mroEnabled}
      mroExpertId={mroExpertId}
      opsExpertIds={opsExpertIds}
      initialRail={mroInitialRail}
      aircraftList={(p) =>
        mro
          .aircraftList(p ?? {})
          .then((r) => ({
            ...r,
            items: r.items.map((item) => ({
              aircraftId: item.aircraftId,
              tailNo: item.tailNo,
              msn: item.msn ?? '',
              model: item.model,
              config: item.config ?? '',
            })),
          }))
      }
      manualList={(p) =>
        mro
          .manualList(p ?? {})
          .then((r) => ({
            ...r,
            items: r.items.map((item) => ({
              manualId: item.manualId,
              title: item.title ?? '',
              docType: item.docType,
              revision: item.revision,
              status: item.status,
              ata: item.ata ?? '',
              sectionCount: item.sectionCount,
            })),
          }))
      }
      onUpsertAircraft={async (input) => {
        const row = await mro.aircraftUpsert(input, { attempt: createMutationAttempt('mro.aircraft.upsert', input) })
        return {
          aircraftId: row.aircraftId,
          tailNo: row.tailNo,
          msn: row.msn ?? '',
          model: row.model,
          config: row.config ?? '',
        }
      }}
      verifiedConnections={async () => {
        const listed = await datasourceBridge.list({}).catch(() => ({ items: [] }))
        return listed.items
          .filter((item) => item.readonlyVerified && item.state === 'active')
          .map((item) => ({ id: item.id, name: item.name, kind: item.kind }))
      }}
      onBindStock={async (input) => {
        const payload = {
          ownerType: 'mro' as const,
          ownerId: 'workbench',
          connectionId: input.connectionId,
          purpose: 'stock' as const,
          tableMap: input.tableMap,
        }
        await datasourceBridge.bind(payload, { attempt: createMutationAttempt('datasource.bind', payload) })
      }}
      onRegisterManual={async (input) => {
        const payload = {
          title: input.title,
          docType: input.docType as 'AMM',
          revision: input.revision,
          status: input.status as 'controlled' | 'uncontrolled' | 'superseded',
          ata: input.ata,
          documents: input.documents,
        }
        const row = await mro.manualRegister(payload, {
          attempt: createMutationAttempt('mro.manual.register', payload),
        })
        return {
          manualId: row.manualId,
          title: row.title ?? '',
          docType: row.docType,
          revision: row.revision,
          status: row.status,
          ata: row.ata ?? '',
          sectionCount: row.sectionCount,
        }
      }}
      onIngestManual={(input) =>
        experts.knowledgeIngest!({
          expertId: input.expertId,
          path: input.path,
          sourceLocator: input.sourceLocator,
          ...(input.mediaType ? { mediaType: input.mediaType } : {}),
        })
      }
      onBuildChecklist={(input) => mro.checklistBuild(input)}
      auditList={() => mro.auditList({})}
      dueList={(p) => mro.dueList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      toolList={(p) => mro.toolList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      lotList={(p) => mro.lotTrace?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      kitList={(p) => mro.kitStaging?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      partsList={(p) => mro.partsStockList?.(p ?? {}) ?? Promise.resolve({ items: [], alternates: [] })}
      planList={(p) => mro.planList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      todoList={(p) => mro.opsTodoList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      constraintList={(p) => mro.planConstraintCheck?.(p ?? {}) ?? Promise.resolve({ violations: [] })}
      onCheckoutTool={async (id) => {
        const payload = { toolId: id, holder: 'workbench' }
        if (!mro.toolCheckout) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.toolCheckout(payload, { attempt: createMutationAttempt('mro.tool.checkout', payload) })
      }}
      onPublishSchedule={async (id) => {
        const payload = { packageId: id }
        if (!mro.planPublish) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.planPublish(payload, { attempt: createMutationAttempt('mro.plan.publish', payload) })
      }}
      onBulletinChain={async (lotId) => {
        if (!mro.lotTrace) throw new Error('机务功能当前不可用，请刷新后重试')
        const listed = await readAllMroPages(
          (p) => mro.lotTrace({ ...p, lotId }),
          () => alive.current,
        )
        const hit = listed.items[0]
        return { tails: hit?.tails ?? [], note: hit?.lotNo ?? '' }
      }}
      componentListFn={(p) => mro.componentList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      pirepListFn={(p) => mro.pirepList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      aogListFn={(p) => mro.aogList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      poListFn={(p) => mro.poList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      triggerListFn={(p) => mro.triggerList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      intervalListFn={(p) => mro.intervalList?.(p ?? {}) ?? Promise.resolve({ items: [] })}
      onAddTool={async (input) => {
        if (!mro.toolUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.toolUpsert(input, { attempt: createMutationAttempt('mro.tool.upsert', input) })
      }}
      onReturnTool={async (id) => {
        const p = { toolId: id }
        if (!mro.toolReturn) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.toolReturn(p, { attempt: createMutationAttempt('mro.tool.return', p) })
      }}
      onAddDue={async (input) => {
        if (!mro.dueUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.dueUpsert(input, { attempt: createMutationAttempt('mro.due.upsert', input) })
      }}
      onRecordUtil={async (input) => {
        if (!mro.utilRecord) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.utilRecord(input, { attempt: createMutationAttempt('mro.util.record', input) })
      }}
      onAddLot={async (input) => {
        if (!mro.lotUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.lotUpsert(input, { attempt: createMutationAttempt('mro.lot.upsert', input) })
      }}
      onRecordUse={async (input) => {
        if (!mro.lotUse) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.lotUse(input, { attempt: createMutationAttempt('mro.lot.use', input) })
      }}
      onDefineKit={async (input) => {
        if (!mro.kitUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.kitUpsert(input, { attempt: createMutationAttempt('mro.kit.upsert', input) })
      }}
      onAddStock={async (input) => {
        if (!mro.partsStockUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.partsStockUpsert(input, { attempt: createMutationAttempt('mro.parts.stock.upsert', input) })
      }}
      onAddAlternate={async (input) => {
        if (!mro.alternateUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.alternateUpsert(input, { attempt: createMutationAttempt('mro.alternate.upsert', input) })
      }}
      onBuildWp={async (input) => {
        if (!mro.workpackageBuild) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.workpackageBuild(input, { attempt: createMutationAttempt('mro.workpackage.build', input) })
      }}
      onAddInterval={async (input) => {
        if (!mro.intervalUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.intervalUpsert(input, { attempt: createMutationAttempt('mro.interval.upsert', input) })
      }}
      onProposeInterval={async (input) => {
        if (!mro.intervalPropose) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.intervalPropose(input, { attempt: createMutationAttempt('mro.interval.propose', input) })
      }}
      onAddSchedule={async (input) => {
        if (!mro.scheduleUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.scheduleUpsert(input, { attempt: createMutationAttempt('mro.schedule.upsert', input) })
      }}
      onSetCapacity={async (input) => {
        if (!mro.capacityUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.capacityUpsert(input, { attempt: createMutationAttempt('mro.capacity.upsert', input) })
      }}
      onAddComponent={async (input) => {
        if (!mro.componentUpsert) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.componentUpsert(input, { attempt: createMutationAttempt('mro.component.upsert', input) })
      }}
      onLifeEvent={async (input) => {
        if (!mro.lifeEvent) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.lifeEvent(input, { attempt: createMutationAttempt('mro.life.event', input) })
      }}
      onDraftPirep={async (input) => {
        if (!mro.pirepDraft) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.pirepDraft(input, { attempt: createMutationAttempt('mro.pirep.draft', input) })
      }}
      onIntakeAog={async (input) => {
        if (!mro.aogIntake) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.aogIntake(input, { attempt: createMutationAttempt('mro.aog.intake', input) })
      }}
      onDraftPo={async (input) => {
        if (!mro.poDraft) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.poDraft(input, { attempt: createMutationAttempt('mro.po.draft', input) })
      }}
      onIssueChem={async (input) => {
        if (!mro.chemIssue) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.chemIssue(input, { attempt: createMutationAttempt('mro.chem.issue', input) })
      }}
      onAddPartsTodo={async (input) => {
        if (!mro.opsTodoAdd) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.opsTodoAdd(input, { attempt: createMutationAttempt('mro.ops.todo.add', input) })
      }}
      onConfirmPirep={async (input) => {
        if (!mro.pirepConfirm) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.pirepConfirm(input, { attempt: createMutationAttempt('mro.pirep.confirm', input) })
      }}
      onConfirmAog={async (input) => {
        if (!mro.aogConfirm) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.aogConfirm(input, { attempt: createMutationAttempt('mro.aog.confirm', input) })
      }}
      onConfirmPo={async (input) => {
        if (!mro.poConfirm) throw new Error('机务功能当前不可用，请刷新后重试')
        return mro.poConfirm(input, { attempt: createMutationAttempt('mro.po.confirm', input) })
      }}
      openChat={(input) => openMroChat({ ...input, ensureProject: ensurePersonalProject, projects, sessions, experts })}
      onAskOpened={(opened) => {
        setDraftSessionIds((values) => new Set(values).add(opened.session.id))
        setTarget({
          project: opened.project,
          session: opened.session,
          personal: true,
          ...(opened.prompt ? { prompt: opened.prompt } : {}),
        })
      }}
    />
  )
}
