import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { ArrowDownNarrowWide, ArrowUpNarrowWide, CheckCheck, Eye, Filter, ListFilter, MoreVertical, Plus, XCircle } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { AppShell } from "@/components/layout/AppShell";
import { ensureSessionsWired, useSessions } from "@/stores/sessions";
import {
  ensureChatsWired,
  fetchChats,
  fetchMessages,
  markChatAsRead,
  setActiveChat,
  setChatStatus,
  useChats,
} from "@/stores/chats";
import type { ChatSummary } from "@/types/chat";
import { ChatList, filterChats } from "@/components/domain/chat/ChatList";
import { ChatView } from "@/components/domain/chat/ChatView";
import { isGroupJid } from "@/components/domain/chat/format";
import { useAuth } from "@/stores/auth";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { assignChat, closeChat } from "@/services/chats";
import { NewChatDialog } from "@/components/domain/chat/NewChatDialog";

type Tab = "open" | "waiting" | "group";

const EMPTY_CHATS: ChatSummary[] = [];

export const ChatsPage = () => {
  const { t } = useTranslation();
  ensureSessionsWired();
  ensureChatsWired();

  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  const pairedSessions = useMemo(() => sessions.filter((s) => s.paired), [sessions]);
  const [pickedSession, setPickedSession] = useState<string | null>(null);
  const [selectedChatSessionId, setSelectedChatSessionId] = useState<string | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();

  // Hidrata pickedSession: se houver mais de uma conexão pareada, inicia em "all"
  // para visualização unificada de todos os atendimentos; caso contrário usa a sessão ativa.
  useEffect(() => {
    if (pickedSession) return;
    if (pairedSessions.length > 1) {
      setPickedSession("all");
      return;
    }
    if (activeId) {
      setPickedSession(activeId);
      return;
    }
    const firstPaired = pairedSessions[0];
    if (firstPaired) setPickedSession(firstPaired.id);
  }, [activeId, sessions, pickedSession, pairedSessions]);

  const sessionId =
    pickedSession ?? (pairedSessions.length > 1 ? "all" : (activeId ?? pairedSessions[0]?.id ?? null));

  // Deep-link: /chats?sid=...&jid=... abre aquela conversa.
  useEffect(() => {
    const sid = searchParams.get("sid");
    const jid = searchParams.get("jid");
    if (!sid && !jid) return;

    const chatsPorSessao = useChats.getState().chatsBySession;
    const targetSid =
      sid ||
      Object.keys(chatsPorSessao).find((s) => (chatsPorSessao[s] ?? []).some((c) => c.chatJid === jid)) ||
      pairedSessions[0]?.id ||
      "";

    if (targetSid && jid) {
      setSelectedChatSessionId(targetSid);
      setActiveChat(targetSid, jid);
      setActiveChat("all", jid);
    }

    const next = new URLSearchParams(searchParams);
    next.delete("sid");
    next.delete("jid");
    setSearchParams(next, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams, pairedSessions]);

  const activeJid = useChats((s) => {
    if (sessionId === "all") {
      return s.activeJidBySession["all"] ?? null;
    }
    return sessionId ? s.activeJidBySession[sessionId] ?? null : null;
  });

  const [tab, setTab] = useState<Tab>("waiting");
  const me = useAuth((s) => s.user);
  const chatsBySession = useChats((s) => s.chatsBySession);

  // Lista de conversas: quando sessionId === "all", consolida todas as conexões pareadas
  // e associa o sessionId de origem a cada card para fácil identificação.
  const chats = useMemo(() => {
    if (sessionId === "all") {
      const allList: ChatSummary[] = [];
      for (const s of pairedSessions) {
        const sessionChats = chatsBySession[s.id] ?? EMPTY_CHATS;
        for (const c of sessionChats) {
          allList.push({ ...c, sessionId: s.id });
        }
      }
      return allList;
    }
    const list = sessionId ? chatsBySession[sessionId] ?? EMPTY_CHATS : EMPTY_CHATS;
    return list.map((c) => ({ ...c, sessionId: sessionId ?? undefined }));
  }, [sessionId, pairedSessions, chatsBySession]);

  const [unreadOnly, setUnreadOnly] = useState(false);
  const [sort, setSort] = useState<"desc" | "asc">("desc");
  const [confirmAssignAll, setConfirmAssignAll] = useState(false);
  const [confirmCloseAll, setConfirmCloseAll] = useState(false);
  const [bulkBusy, setBulkBusy] = useState(false);
  const [newChatOpen, setNewChatOpen] = useState(false);

  useEffect(() => {
    if (sessionId === "all") {
      pairedSessions.forEach((s) => void fetchChats(s.id));
    } else if (sessionId) {
      void fetchChats(sessionId);
    }
  }, [sessionId, pairedSessions]);

  // Refetch quando o usuário volta para a aba ou re-foca a janela.
  useEffect(() => {
    if (!sessionId) return;
    const refetch = () => {
      if (document.visibilityState === "visible") {
        if (sessionId === "all") {
          pairedSessions.forEach((s) => void fetchChats(s.id));
        } else {
          void fetchChats(sessionId);
        }
      }
    };
    window.addEventListener("focus", refetch);
    document.addEventListener("visibilitychange", refetch);
    return () => {
      window.removeEventListener("focus", refetch);
      document.removeEventListener("visibilitychange", refetch);
    };
  }, [sessionId, pairedSessions]);

  // Conversa ativa selecionada
  const activeChat = useMemo(
    () =>
      activeJid
        ? chats.find(
            (c) =>
              c.chatJid === activeJid &&
              (!selectedChatSessionId || c.sessionId === selectedChatSessionId),
          ) ??
          chats.find((c) => c.chatJid === activeJid) ??
          null
        : null,
    [chats, activeJid, selectedChatSessionId],
  );

  // Conexão efetiva para o ChatView (usa a conexão específica da conversa selecionada)
  const effectiveSessionId = useMemo(() => {
    if (sessionId !== "all" && sessionId) return sessionId;
    if (selectedChatSessionId) return selectedChatSessionId;
    if (activeChat?.sessionId) return activeChat.sessionId;
    if (activeJid) {
      for (const s of pairedSessions) {
        if ((chatsBySession[s.id] ?? []).some((c) => c.chatJid === activeJid)) {
          return s.id;
        }
      }
    }
    return pairedSessions[0]?.id ?? "";
  }, [sessionId, selectedChatSessionId, activeChat, activeJid, pairedSessions, chatsBySession]);

  useEffect(() => {
    if (effectiveSessionId && activeJid) {
      void fetchMessages(effectiveSessionId, activeJid);
      markChatAsRead(effectiveSessionId, activeJid);
    }
  }, [effectiveSessionId, activeJid]);

  // Follow real-time status changes of the active chat (SSE chat-meta).
  const activeStatus = activeChat?.status;
  const activeIsGroup = activeChat ? activeChat.isGroup || isGroupJid(activeChat.chatJid) : false;
  useEffect(() => {
    if (!activeChat) return;
    if (activeIsGroup) {
      if (tab !== "group") setTab("group");
      return;
    }
    if (activeStatus === "open" && tab !== "open") setTab("open");
    else if ((activeStatus === "waiting" || activeStatus === "closed") && tab !== "waiting") setTab("waiting");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeStatus, activeIsGroup, activeChat?.chatJid]);

  const tabCounts = useMemo(() => {
    const counts = { open: 0, waiting: 0, group: 0 };
    for (const c of chats) {
      const isGroup = c.isGroup || isGroupJid(c.chatJid);
      if (isGroup) {
        const status = c.status ?? "group";
        if (status !== "closed") counts.group += 1;
      }
      else if ((c.status ?? "waiting") === "waiting") counts.waiting += 1;
      else if ((c.status ?? "") === "open" && (!me?.id || !c.assignedUserId || c.assignedUserId === me.id))
        counts.open += 1;
    }
    return counts;
  }, [chats, me?.id]);

  // Conversas filtradas na aba atual (para ações em lote)
  const targetedForBulk = useMemo(
    () => filterChats(chats, tab, me?.id ?? null, unreadOnly),
    [chats, tab, me?.id, unreadOnly],
  );

  const TAB_LABEL: Record<Tab, string> = {
    open: t("pages.chats.tabs.open"),
    waiting: t("pages.chats.tabs.waiting"),
    group: t("pages.chats.tabs.group"),
  };

  // Aceitar todas as conversas aguardando de 1 vez
  const handleBulkAssign = async () => {
    if (targetedForBulk.length === 0) return;
    setBulkBusy(true);
    const total = targetedForBulk.length;
    const tId = toast.loading(
      t("pages.chats.bulkAssignLoading", {
        defaultValue: "Aceitando {{count}} atendimento(s)…",
        count: total,
      }),
    );
    const queue = [...targetedForBulk];
    let ok = 0;
    let fail = 0;
    const worker = async () => {
      while (queue.length) {
        const c = queue.shift();
        if (!c) break;
        const targetSid = c.sessionId || (sessionId !== "all" ? sessionId : "");
        if (!targetSid) continue;
        try {
          await assignChat(targetSid, c.chatJid);
          setChatStatus(targetSid, c.chatJid, "open", me?.id ?? null);
          ok += 1;
        } catch {
          fail += 1;
        }
      }
    };
    await Promise.all(Array.from({ length: Math.min(6, targetedForBulk.length) }, worker));
    setBulkBusy(false);
    toast.dismiss(tId);
    if (fail === 0) {
      toast.success(
        t("pages.chats.bulkAssignSuccess", {
          defaultValue: "{{count}} atendimento(s) aceito(s) com sucesso!",
          count: ok,
        }),
      );
      setTab("open");
    } else {
      toast.error(
        t("pages.chats.bulkAssignPartial", {
          defaultValue: "Aceitos {{ok}}, falharam {{fail}}",
          ok,
          fail,
        }),
      );
    }
    if (sessionId === "all") {
      pairedSessions.forEach((s) => void fetchChats(s.id));
    } else if (sessionId) {
      void fetchChats(sessionId);
    }
  };

  // Finalizar todos os atendimentos da aba atual
  const handleBulkClose = async () => {
    if (targetedForBulk.length === 0) return;
    setBulkBusy(true);
    const total = targetedForBulk.length;
    const tId = toast.loading(
      t("pages.chats.bulkLoading", {
        defaultValue: "Finalizando {{count}} conversa(s)…",
        count: total,
      }),
    );
    const queue = [...targetedForBulk];
    let ok = 0;
    let fail = 0;
    const worker = async () => {
      while (queue.length) {
        const c = queue.shift();
        if (!c) break;
        const targetSid = c.sessionId || (sessionId !== "all" ? sessionId : "");
        if (!targetSid) continue;
        try {
          await closeChat(targetSid, c.chatJid, "encerramento em massa");
          setChatStatus(targetSid, c.chatJid, "closed", null);
          ok += 1;
        } catch {
          fail += 1;
        }
      }
    };
    await Promise.all(Array.from({ length: Math.min(6, targetedForBulk.length) }, worker));
    setBulkBusy(false);
    toast.dismiss(tId);
    if (fail === 0) {
      toast.success(
        t("pages.chats.bulkSuccess", {
          defaultValue: "{{count}} conversa(s) finalizada(s)",
          count: ok,
        }),
      );
    } else {
      toast.error(
        t("pages.chats.bulkPartial", {
          defaultValue: "Finalizadas {{ok}}, falharam {{fail}}",
          ok,
          fail,
        }),
      );
    }
    if (sessionId === "all") {
      pairedSessions.forEach((s) => void fetchChats(s.id));
    } else if (sessionId) {
      void fetchChats(sessionId);
    }
  };

  if (!sessionId) {
    return (
      <AppShell>
        <div className="grid h-full place-items-center text-sm text-muted-foreground">
          {t("pages.chats.pairSessionPrompt")}
        </div>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <div className="flex h-full min-h-0 gap-3">
        <div className="flex w-96 shrink-0 flex-col overflow-hidden rounded-2xl border bg-card shadow-sm">
          {pairedSessions.length > 1 && (
            <div className="border-b p-2">
              <select
                className="w-full rounded-md border bg-background px-2 py-1.5 text-sm font-medium"
                value={sessionId ?? "all"}
                onChange={(e) => {
                  const val = e.target.value;
                  setPickedSession(val);
                  setSelectedChatSessionId(val === "all" ? null : val);
                  setActiveChat(val, null);
                  if (val === "all") {
                    setActiveChat("all", null);
                  }
                }}
              >
                <option value="all">
                  {t("pages.chats.allConnections", { defaultValue: "Todas as conexões" })}
                </option>
                {pairedSessions.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="flex items-stretch border-b text-xs font-medium">
            <div className="flex flex-1">
            {([
              { id: "open", label: t("pages.chats.tabs.open") },
              { id: "waiting", label: t("pages.chats.tabs.waiting") },
              { id: "group", label: t("pages.chats.tabs.group") },
            ] as { id: Tab; label: string }[]).map((t) => {
              const count = tabCounts[t.id];
              return (
                <button
                  key={t.id}
                  onClick={() => setTab(t.id)}
                  className={`flex flex-1 items-center justify-center gap-1.5 px-2 py-2 ${tab === t.id ? "border-b-2 border-primary text-foreground" : "text-muted-foreground hover:bg-muted/50"}`}
                >
                  <span>{t.label}</span>
                  {count > 0 && (
                    <span className="grid h-4 min-w-[1rem] place-items-center rounded-full bg-primary px-1 text-[10px] font-semibold leading-none text-primary-foreground">
                      {count > 99 ? "99+" : count}
                    </span>
                  )}
                </button>
              );
            })}
            </div>
            <DropdownMenu>
              <button
                type="button"
                onClick={() => setNewChatOpen(true)}
                aria-label="Abrir atendimento"
                title="Abrir atendimento"
                className="grid w-9 shrink-0 place-items-center border-l text-muted-foreground hover:bg-muted/50 hover:text-foreground"
              >
                <Plus className="h-4 w-4" />
              </button>
              <DropdownMenuTrigger
                aria-label={t("pages.chats.listOptionsAria")}
                className="grid w-9 shrink-0 place-items-center border-l text-muted-foreground hover:bg-muted/50 hover:text-foreground"
              >
                <MoreVertical className="h-4 w-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56">
                <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
                  {t("actions.filter")}
                </DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => setUnreadOnly(false)} className="gap-2">
                  <Eye className="h-4 w-4" /> {t("actions.viewAll")}
                  {!unreadOnly && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setUnreadOnly(true)} className="gap-2">
                  <Filter className="h-4 w-4" /> {t("actions.unreadOnly")}
                  {unreadOnly && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuLabel className="text-xs uppercase tracking-wide text-muted-foreground">
                  {t("actions.sort")}
                </DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => setSort("desc")} className="gap-2">
                  <ArrowDownNarrowWide className="h-4 w-4" /> {t("actions.sortNewest")}
                  {sort === "desc" && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => setSort("asc")} className="gap-2">
                  <ArrowUpNarrowWide className="h-4 w-4" /> {t("actions.sortOldest")}
                  {sort === "asc" && <CheckCheck className="ml-auto h-3.5 w-3.5 text-primary" />}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                {tab === "waiting" && (
                  <DropdownMenuItem
                    disabled={bulkBusy || targetedForBulk.length === 0}
                    onSelect={() => setConfirmAssignAll(true)}
                    className="gap-2 text-emerald-600 dark:text-emerald-400 focus:text-emerald-600 dark:focus:text-emerald-400 font-medium cursor-pointer"
                  >
                    <CheckCheck className="h-4 w-4" />
                    {t("pages.chats.bulkAssignCount", {
                      defaultValue: "Aceitar todos ({{count}})",
                      count: targetedForBulk.length,
                    })}
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem
                  disabled={bulkBusy || targetedForBulk.length === 0}
                  onSelect={() => setConfirmCloseAll(true)}
                  className="gap-2 text-destructive focus:text-destructive font-medium cursor-pointer"
                >
                  <XCircle className="h-4 w-4" />
                  {t("pages.chats.bulkCount", {
                    count: targetedForBulk.length,
                    defaultValue: "Finalizar todos ({{count}})",
                  })}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          {/* Barra de Ações Rápidas em Lote (Aceitar todos / Finalizar todos) */}
          {targetedForBulk.length > 0 && (
            <div className="flex items-center justify-between gap-2 border-b bg-muted/40 px-3 py-1.5 text-xs">
              <span className="truncate text-[11px] font-medium text-muted-foreground">
                {tab === "waiting"
                  ? `${targetedForBulk.length} aguardando`
                  : `${targetedForBulk.length} em atendimento`}
              </span>
              <div className="flex items-center gap-1.5 shrink-0">
                {tab === "waiting" && (
                  <button
                    type="button"
                    disabled={bulkBusy}
                    onClick={() => setConfirmAssignAll(true)}
                    className="inline-flex items-center gap-1 rounded-md bg-emerald-600/10 px-2 py-1 text-[11px] font-semibold text-emerald-600 transition hover:bg-emerald-600/20 dark:text-emerald-400 dark:hover:bg-emerald-500/20 disabled:opacity-50"
                  >
                    <CheckCheck className="h-3 w-3" />
                    Aceitar todos ({targetedForBulk.length})
                  </button>
                )}
                <button
                  type="button"
                  disabled={bulkBusy}
                  onClick={() => setConfirmCloseAll(true)}
                  className="inline-flex items-center gap-1 rounded-md bg-destructive/10 px-2 py-1 text-[11px] font-semibold text-destructive transition hover:bg-destructive/20 disabled:opacity-50"
                >
                  <XCircle className="h-3 w-3" />
                  Finalizar todos ({targetedForBulk.length})
                </button>
              </div>
            </div>
          )}

          {(unreadOnly || sort === "asc") && (
            <div className="flex items-center gap-2 border-b bg-muted/30 px-3 py-1.5 text-[11px] text-muted-foreground">
              <ListFilter className="h-3 w-3" />
              <span className="truncate">
                {unreadOnly ? t("pages.chats.filterUnreadHint") : ""}
                {unreadOnly && sort === "asc" ? " · " : ""}
                {sort === "asc" ? t("pages.chats.filterOldestHint") : ""}
              </span>
              <button
                type="button"
                onClick={() => {
                  setUnreadOnly(false);
                  setSort("desc");
                }}
                className="ml-auto text-[10px] uppercase tracking-wider text-primary hover:underline"
              >
                {t("actions.clear")}
              </button>
            </div>
          )}
          <ChatList
            sessionId={sessionId}
            chats={chats}
            activeJid={activeJid}
            tab={tab}
            myId={me?.id ?? null}
            unreadOnly={unreadOnly}
            sort={sort}
            onSelect={(jid, chatSessionId) => {
              const targetSid =
                chatSessionId ||
                (sessionId === "all" ? chats.find((c) => c.chatJid === jid)?.sessionId : sessionId) ||
                "";
              if (targetSid) {
                setSelectedChatSessionId(targetSid);
                setActiveChat(targetSid, jid);
              }
              if (sessionId === "all") {
                setActiveChat("all", jid);
              }
            }}
            onStatusChange={(status) => {
              if (status === "open") setTab("open");
              else if (status === "waiting" || status === "closed") setTab("waiting");
            }}
          />
        </div>
        <div className="flex min-w-0 flex-1 overflow-hidden rounded-2xl border bg-card shadow-sm">
          <ChatView
            sessionId={effectiveSessionId}
            chatJid={activeJid}
            onStatusChange={(status) => {
              if (status === "open") setTab("open");
              else if (status === "closed" || status === "waiting") setTab("waiting");
            }}
          />
        </div>
      </div>
      <ConfirmDialog
        open={confirmAssignAll}
        onOpenChange={setConfirmAssignAll}
        title={t("pages.chats.assignAllTitle", {
          defaultValue: `Aceitar todos da aba "${TAB_LABEL[tab]}"?`,
        })}
        description={t("pages.chats.assignAllDescription", {
          defaultValue: `Deseja aceitar todos os {{count}} atendimentos aguardando? Eles serão atribuídos a você e movidos para a aba Atendendo.`,
          count: targetedForBulk.length,
        })}
        confirmLabel={t("pages.chats.assignAllConfirm", { defaultValue: "Aceitar todos" })}
        onConfirm={handleBulkAssign}
      />
      <ConfirmDialog
        open={confirmCloseAll}
        onOpenChange={setConfirmCloseAll}
        title={t("pages.chats.closeAllTitle", { tab: TAB_LABEL[tab] })}
        description={t("pages.chats.closeAllDescription", {
          count: targetedForBulk.length,
          unreadHint: unreadOnly ? t("pages.chats.closeAllDescription_unread") : "",
        })}
        confirmLabel={t("actions.closeAll", { defaultValue: "Finalizar todos" })}
        destructive
        onConfirm={handleBulkClose}
      />
      <NewChatDialog
        open={newChatOpen}
        onOpenChange={setNewChatOpen}
        sessionId={sessionId === "all" ? (pairedSessions[0]?.id ?? "") : sessionId}
        onOpened={() => {
          setTab("waiting");
        }}
      />
    </AppShell>
  );
};