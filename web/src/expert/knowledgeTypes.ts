export type KnowledgeSource = {
  sourceId: string; collectionId: string; path: string; mediaType: string; sourceLocator: string
  sha256: string; state: 'fresh'|'refreshing'|'stale'|'missing'|'failed'; version: number; revision: number
  checkedAt: string; error: string; createdAt: string
  nextBeforeVersion?: number
  versions: Array<{version:number;sha256:string;state:string;error:string;createdAt:string}>
}
export type KnowledgeIngest = (payload: {expertId:string;path:string;mediaType?:string;sourceLocator?:string;expectedRevision?:number}) => Promise<{documents:Array<{indexState?:string;preview?:string[];failReason?:string}>}>

export type KnowledgeStats = {
  collectionId?: string
  documentCount: number
  readyCount: number
  chunkCount: number
  nodeCount: number
  memoryCount: number
  missing: boolean
  sources?: KnowledgeSource[]
  nextSourceCursor?: string
}


export type KnowledgeGetPayload={expertId:string;sourcesAfter?:string;historySourceId?:string;historyBeforeVersion?:number}
