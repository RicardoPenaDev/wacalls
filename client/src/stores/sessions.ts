import { create } from "zustand";
import { eventStream, type BrokerEvent } from "@/lib/event-stream";
import { getClientId } from "@/lib/client-id";
import { listSessions } from "@/services/sessions";
import type { SessionInfo } from "@/types/session";

type State = {
  sessions: SessionInfo[];
  qrs: Record<string, string>;
  activeId: string | null;
};

export const useSessions = create<State>(() => ({ sessions: [], qrs: {}, activeId: null }));

export const setActiveSession = (id: string): void => useSessions.setState({ activeId: id });

export const resetSessionsStore = (): void => useSessions.setState({ sessions: [], qrs: {}, activeId: null });

const pickActive = (sessions: SessionInfo[], current: string | null): string | null => {
  if (current && sessions.some((s) => s.id === current)) return current;
  return sessions[0]?.id ?? null;
};

let wired = false;
let inFlightSessions: Promise<void> | null = null;
let lastSessionsFetchTs = 0;

export const ensureSessionsWired = (force = false): void => {
  eventStream.connect(getClientId());
  const now = Date.now();
  if (force || (!inFlightSessions && now - lastSessionsFetchTs > 4000)) {
    lastSessionsFetchTs = now;
    inFlightSessions = listSessions()
      .then((sessions) => {
        useSessions.setState((s) => {
          const old = s.sessions;
          if (
            old.length === sessions.length &&
            old.every(
              (o, i) =>
                o.id === sessions[i].id &&
                o.paired === sessions[i].paired &&
                o.state === sessions[i].state &&
                o.name === sessions[i].name,
            )
          ) {
            return s;
          }
          return { sessions, activeId: pickActive(sessions, s.activeId) };
        });
      })
      .catch(() => {})
      .finally(() => {
        inFlightSessions = null;
      });
  }
  if (wired) return;
  wired = true;
  eventStream.on((ev: BrokerEvent) => {
    if (ev.type === "session-list") {
      useSessions.setState((s) => {
        const ids = new Set(ev.sessions.map((x) => x.id));
        const qrs: Record<string, string> = {};
        for (const [id, qr] of Object.entries(s.qrs)) if (ids.has(id)) qrs[id] = qr;
        return { sessions: ev.sessions, qrs, activeId: pickActive(ev.sessions, s.activeId) };
      });
    } else if (ev.type === "session-qr") {
      useSessions.setState((s) => ({ qrs: { ...s.qrs, [ev.sessionId]: ev.qr } }));
    } else if (ev.type === "auth-state") {
      useSessions.setState((s) => {
        const sessions = s.sessions.map((x) =>
          x.id === ev.sessionId ? { ...x, state: ev.state, paired: ev.paired } : x,
        );
        const qrs = { ...s.qrs };
        if (ev.paired) delete qrs[ev.sessionId];
        else if (ev.qr) qrs[ev.sessionId] = ev.qr;
        return { sessions, qrs };
      });
    }
  });
};
