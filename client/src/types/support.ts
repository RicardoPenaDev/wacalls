export type SupportSyncState =
  | "processing"
  | "synced"
  | "retryable_error"
  | "unknown"
  | "failed";

export type DeviceMatchStatus =
  | "pending"
  | "matched"
  | "conflict"
  | "missing_glpi"
  | "missing_tactical"
  | "disabled"
  | string;

export interface SupportRequestDTO {
  id: string;
  sessionId: string;
  chatJid: string;
  title: string;
  description: string;
  requesterName: string;
  hostnameInformed: string;
  hostnameNormalized: string;
  deviceBindingId?: string | null;
  syncState: SupportSyncState;
  lastErrorCode?: string | null;
  attemptCount: number;
  glpiTicketId?: string | null;
  glpiTicketHref?: string | null;
  categoryId?: string | null;
  locationId?: string | null;
  priority: number;
  createdAt: number;
  updatedAt: number;
  processedAt?: number | null;
}

export interface DeviceBindingDTO {
  id: string;
  hostname: string;
  hostnameNormalized: string;
  glpiComputerId?: string | null;
  tacticalAgentId?: string | null;
  tacticalClientId?: string | null;
  tacticalSiteId?: string | null;
  sectorCode?: string | null;
  patrimonio?: string | null;
  matchStatus: DeviceMatchStatus;
  lastVerifiedAt: number;
  createdAt: number;
  updatedAt: number;
}

export interface TacticalAgentSummary {
  id?: string;
  hostname?: string;
  status?: "online" | "offline" | "overdue" | string;
  operating_system?: string;
  operatingSystem?: string;
  public_ip?: string;
  publicIp?: string;
  local_ips?: string[] | string;
  localIps?: string[];
  last_seen?: string;
  lastSeen?: string;
  logged_username?: string;
  loggedUsername?: string;
}

export interface ConversationSupportResponse {
  supportRequests: SupportRequestDTO[];
  currentSupportRequest?: SupportRequestDTO | null;
}

export interface SupportTicketResponseEnvelope {
  supportRequest: SupportRequestDTO;
  device?: DeviceBindingDTO | null;
  glpiComputer?: unknown | null;
  tacticalAgent?: TacticalAgentSummary | null;
  warnings: string[];
  glpiContextUpdated?: boolean;
}

export interface DeviceDetailResponse {
  device: DeviceBindingDTO;
  glpiComputer?: unknown | null;
  tacticalAgent?: TacticalAgentSummary | null;
  warnings: string[];
}

export interface SearchDevicesResponse {
  devices: DeviceBindingDTO[];
}

export interface CreateSupportTicketPayload {
  requesterName: string;
  title: string;
  description: string;
  deviceBindingId?: string;
  hostname?: string;
  categoryId?: string;
  locationId?: string;
  priority?: number;
}

export interface UpdateDevicePayload {
  deviceBindingId: string;
}

export interface ReconcilePayload {
  outcome: "synced" | "safe_to_retry" | "processing_orphaned";
  glpiTicketId?: string;
}

export interface SupportErrorDetail {
  code: string;
  message: string;
  retryAfterSeconds?: number;
}
