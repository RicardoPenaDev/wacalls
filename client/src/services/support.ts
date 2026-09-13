import { apiUrl } from "@/lib/api-base";
import type {
  ConversationSupportResponse,
  CreateSupportTicketPayload,
  DeviceDetailResponse,
  ReconcilePayload,
  SearchDevicesResponse,
  SupportTicketResponseEnvelope,
  UpdateDevicePayload,
} from "@/types/support";

export class SupportApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly retryAfterSeconds?: number;

  constructor(status: number, code: string, message: string, retryAfterSeconds?: number) {
    super(message || code || `HTTP ${status}`);
    this.name = "SupportApiError";
    this.status = status;
    this.code = code;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let code = "request_failed";
    let message = `HTTP ${res.status}`;
    let retryAfterSeconds: number | undefined;

    const retryHeader = res.headers.get("Retry-After");
    if (retryHeader) {
      const parsed = parseInt(retryHeader, 10);
      if (!Number.isNaN(parsed) && parsed > 0) {
        retryAfterSeconds = parsed;
      }
    }

    try {
      const body = await res.json();
      if (body?.error) {
        if (typeof body.error === "object") {
          if (body.error.code) code = body.error.code;
          if (body.error.message) message = body.error.message;
          if (body.error.retryAfterSeconds) retryAfterSeconds = body.error.retryAfterSeconds;
        } else if (typeof body.error === "string") {
          message = body.error;
          code = body.error;
        }
      }
    } catch {
      // JSON parse failed or body is empty; fallback to status text
      if (res.statusText) {
        message = res.statusText;
      }
    }

    throw new SupportApiError(res.status, code, message, retryAfterSeconds);
  }

  if (res.status === 204) {
    return undefined as unknown as T;
  }
  return (await res.json()) as T;
}

export async function getChatSupportContext(
  sessionId: string,
  chatJid: string,
  signal?: AbortSignal,
): Promise<ConversationSupportResponse> {
  const encSid = encodeURIComponent(sessionId);
  const encJid = encodeURIComponent(chatJid);
  const res = await fetch(apiUrl(`/api/sessions/${encSid}/chats/${encJid}/support`), {
    method: "GET",
    credentials: "include",
    signal,
  });
  return handleResponse<ConversationSupportResponse>(res);
}

export async function createChatSupportTicket(
  sessionId: string,
  chatJid: string,
  idempotencyKey: string,
  payload: CreateSupportTicketPayload,
  signal?: AbortSignal,
): Promise<SupportTicketResponseEnvelope> {
  const encSid = encodeURIComponent(sessionId);
  const encJid = encodeURIComponent(chatJid);
  const res = await fetch(apiUrl(`/api/sessions/${encSid}/chats/${encJid}/support/ticket`), {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": idempotencyKey,
    },
    body: JSON.stringify(payload),
    signal,
  });
  return handleResponse<SupportTicketResponseEnvelope>(res);
}

export async function getSupportRequest(
  id: string,
  signal?: AbortSignal,
): Promise<SupportTicketResponseEnvelope> {
  const encId = encodeURIComponent(id);
  const res = await fetch(apiUrl(`/api/support/requests/${encId}`), {
    method: "GET",
    credentials: "include",
    signal,
  });
  return handleResponse<SupportTicketResponseEnvelope>(res);
}

export async function retrySupportRequest(
  id: string,
  signal?: AbortSignal,
): Promise<SupportTicketResponseEnvelope> {
  const encId = encodeURIComponent(id);
  const res = await fetch(apiUrl(`/api/support/requests/${encId}/retry`), {
    method: "POST",
    credentials: "include",
    signal,
  });
  return handleResponse<SupportTicketResponseEnvelope>(res);
}

export async function reconcileSupportRequest(
  id: string,
  payload: ReconcilePayload,
  signal?: AbortSignal,
): Promise<SupportTicketResponseEnvelope> {
  const encId = encodeURIComponent(id);
  const res = await fetch(apiUrl(`/api/support/requests/${encId}/reconcile`), {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
    signal,
  });
  return handleResponse<SupportTicketResponseEnvelope>(res);
}

export async function searchSupportDevices(
  query: string,
  limit = 20,
  signal?: AbortSignal,
): Promise<SearchDevicesResponse> {
  const params = new URLSearchParams();
  if (query.trim()) {
    params.set("query", query.trim());
  }
  if (limit > 0) {
    params.set("limit", String(limit));
  }
  const qs = params.toString();
  const path = qs ? `/api/support/devices?${qs}` : "/api/support/devices";
  const res = await fetch(apiUrl(path), {
    method: "GET",
    credentials: "include",
    signal,
  });
  return handleResponse<SearchDevicesResponse>(res);
}

export async function getSupportDevice(
  id: string,
  signal?: AbortSignal,
): Promise<DeviceDetailResponse> {
  const encId = encodeURIComponent(id);
  const res = await fetch(apiUrl(`/api/support/devices/${encId}`), {
    method: "GET",
    credentials: "include",
    signal,
  });
  return handleResponse<DeviceDetailResponse>(res);
}

export async function updateSupportRequestDevice(
  id: string,
  payload: UpdateDevicePayload,
  signal?: AbortSignal,
): Promise<SupportTicketResponseEnvelope> {
  const encId = encodeURIComponent(id);
  const res = await fetch(apiUrl(`/api/support/requests/${encId}/device`), {
    method: "PUT",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
    signal,
  });
  return handleResponse<SupportTicketResponseEnvelope>(res);
}
