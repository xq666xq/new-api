export type ChannelHealth = 'none' | 'healthy' | 'degraded' | 'down'

export type BucketState = ChannelHealth

export interface RecentCheck {
  health: BucketState
  total: number
  success: number
  startAt: number
  endAt: number
}

export interface ChannelMonitorResult {
  id: number
  channelId: number
  modelName: string
  questionId: number
  questionContent: string
  success: boolean
  latencyMs: number
  statusCode: number
  errorMessage: string
  checkedAt: number
}

export interface ChannelStatusRow {
  id: string
  channelId: number
  name: string
  type: string
  group: string
  tag: string
  health: ChannelHealth
  model: string
  successRate: number
  avgResponseMs: number
  requests: number
  lastCheckedAt: number
  lastTtftMs: number
  lastLatencyMs: number
  /**
   * Current routing state of this channel+model pair from the abilities table:
   * whether the model is enabled on this channel and the priority used to order
   * it during selection. Admin (channel) view only; the aggregated member view
   * leaves these at their defaults (modelEnabled false, modelPriority 0) since it
   * hides channel identity.
   */
  modelEnabled: boolean
  modelPriority: number
  recentChecks: RecentCheck[]
}

/** Selectable status time windows; keys are sent verbatim to the backend. */
export type StatusRange = '1h' | '6h' | '12h' | '24h' | '7d'
