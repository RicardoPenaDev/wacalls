import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Bell,
  Clock,
  Code2,
  CreditCard,
  Phone,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  Smartphone,
  Trash2,
  Wifi,
  WifiOff,
  WrenchIcon,
  X,
} from "lucide-react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EvolutionAlertModal } from "@/components/domain/session/EvolutionAlertModal";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { CreateConnectionModal } from "@/components/domain/session/CreateConnectionModal";
import { EditConnectionModal } from "@/components/domain/session/EditConnectionModal";
import { QRDialog } from "@/components/domain/session/QRDialog";
import { DisconnectDialog } from "@/components/domain/session/DisconnectDialog";
import { PaymentDialog } from "@/components/domain/session/PaymentDialog";
import { AccountHealthDialog } from "@/components/domain/session/AccountHealthDialog";
import { deleteSession, pairSession } from "@/services/sessions";
import { getEvolutionAlert } from "@/services/settings";
import { ensureSessionsWired, useSessions } from "@/stores/sessions";
import type { SessionInfo } from "@/types/session";
import { formatBRL, getInstancePlan } from "@/lib/instance-plan";
import { formatPhone } from "@/lib/phone-format";
import {
  getReceiveCalls,
  setReceiveCalls as persistReceiveCalls,
} from "@/lib/connection-prefs";




// ============================================================================
// Per-instance card. Heavy, but mirrors the reference closely: icon, name +
// pencil hint, masked phone, right-side status pills, footer with two toggles
// (Receber chamada / Gravar chamadas) and the action buttons.
// ============================================================================
const InstanceRow = ({
  s,
  onConfigure,
  onConnect,
  onDisconnect,
  onPay,
  onRestart,
  onDelete,
}: {
  s: SessionInfo;
  onConfigure: () => void;
  onConnect: () => void;
  onDisconnect: () => void;
  onPay: () => void;
  onRestart: () => void;
  onDelete: () => void;
}) => {
  const { t } = useTranslation();
  const plan = getInstancePlan(s.id);
  const isPaid = plan.plan === "paid";
  const isUnpaid = isPaid && !plan.paid;
  const isConnected = s.state === "open" && s.paired;
  const isRestricted = s.state === "logged_out" && s.paired;

  const [receiveCalls, setReceiveCallsState] = useState(() => getReceiveCalls(s.id));
  const [healthOpen, setHealthOpen] = useState(false);
  const setReceiveCalls = (v: boolean) => {
    setReceiveCallsState(v);
    persistReceiveCalls(s.id, v);
  };


  return (
    <div className="rounded-xl border bg-card p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          {s.avatarUrl ? (
            <img
              src={s.avatarUrl}
              alt={s.name}
              onError={(e) => {
                // Fallback when the cached avatar URL 404s — replace the <img>
                // with the placeholder icon so the row keeps rendering.
                (e.currentTarget as HTMLImageElement).style.display = "none";
              }}
              className="h-10 w-10 shrink-0 rounded-lg object-cover"
            />
          ) : (
            <span className="grid h-10 w-10 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground">
              <Code2 className="h-4 w-4" />
            </span>
          )}
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <h3 className="truncate text-sm font-semibold">{s.name}</h3>
              <button
                type="button"
                onClick={onConfigure}
                aria-label={t("actions.rename")}
                className="text-muted-foreground hover:text-foreground"
              >
                <WrenchIcon className="h-3 w-3" />
              </button>
            </div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {s.jid ? formatPhone(s.jid) : <em className="italic">{t("pages.connections.noNumberLinked")}</em>}
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-1.5">
          {isPaid ? (
            <button
              type="button"
              onClick={() => setHealthOpen(true)}
              title={t("pages.connections.openHealth", { defaultValue: "Ver saúde da conta" })}
              className="inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 text-[11px] font-semibold text-amber-500 transition hover:bg-amber-500/25"
            >
              <Clock className="h-3 w-3" /> {formatBRL(plan.price)}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => setHealthOpen(true)}
              title={t("pages.connections.openHealth", { defaultValue: "Ver saúde da conta" })}
              className="inline-flex items-center gap-1 rounded-full bg-sky-500/15 px-2 py-0.5 text-[11px] font-semibold text-sky-400 transition hover:bg-sky-500/25"
            >
              {t("status.free")}
            </button>
          )}
          {isConnected ? (
            <button
              type="button"
              onClick={() => setHealthOpen(true)}
              title={t("pages.connections.openHealth", { defaultValue: "Ver saúde da conta" })}
              className="inline-flex items-center gap-1 rounded-full border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 text-[11px] font-semibold text-emerald-400 transition hover:bg-emerald-500/20"
            >
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" /> {t("status.connected")}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => setHealthOpen(true)}
              title={t("pages.connections.openHealth", { defaultValue: "Ver saúde da conta" })}
              className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[11px] font-semibold text-muted-foreground transition hover:bg-muted/70"
            >
              <WifiOff className="h-3 w-3" /> {t("status.disconnected")}
            </button>
          )}
          {isRestricted ? (
            <button
              type="button"
              onClick={() => setHealthOpen(true)}
              title={t("pages.connections.openHealth", { defaultValue: "Ver saúde da conta" })}
              className="inline-flex items-center gap-1 rounded-full border border-rose-500/40 bg-rose-500/10 px-2 py-0.5 text-[11px] font-semibold text-rose-400 transition hover:bg-rose-500/20"
            >
              {t("status.restriction")}
            </button>
          ) : null}
        </div>
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t pt-3">
        <div className="flex flex-wrap items-center gap-4">
          <label className="flex cursor-pointer items-center gap-2 text-xs">
            <Phone className="h-3.5 w-3.5 text-emerald-500" />
            <span className="font-medium">{t("pages.connections.receiveCalls")}</span>
            <Toggle on={receiveCalls} onChange={setReceiveCalls} />
          </label>


        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" onClick={onConfigure}>
            <Settings2 className="h-3.5 w-3.5" /> {t("actions.configure")}
          </Button>
          {isConnected ? (
            <>
              <Button size="sm" variant="outline" onClick={onRestart}>
                <RefreshCw className="h-3.5 w-3.5" /> {t("actions.restart")}
              </Button>
              <Button
                size="sm"
                variant="outline"
                className="border-rose-500/30 text-rose-400 hover:bg-rose-500/10 hover:text-rose-300"
                onClick={onDisconnect}
              >
                <WifiOff className="h-3.5 w-3.5" /> {t("actions.disconnect")}
              </Button>
            </>
          ) : isUnpaid ? (
            <Button size="sm" variant="outline" onClick={onPay}>
              <CreditCard className="h-3.5 w-3.5" /> {t("actions.payToConnect")}
            </Button>
          ) : (
            <Button size="sm" onClick={onConnect}>
              <Wifi className="h-3.5 w-3.5" /> {t("actions.connect")}
            </Button>
          )}
          <button
            type="button"
            onClick={onDelete}
            aria-label={t("pages.connections.deleteAria")}
            className="rounded-md border p-1.5 text-muted-foreground transition hover:bg-destructive/10 hover:text-destructive"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      {isConnected ? (
        <div className="mt-2 text-[11px] text-muted-foreground">
          {t("pages.connections.connectedSince", {
            date: new Date().toLocaleDateString(),
            time: new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
          })}
        </div>
      ) : null}
      <AccountHealthDialog
        open={healthOpen}
        onOpenChange={setHealthOpen}
        session={s}
      />
    </div>
  );
};

const Toggle = ({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) => (
  <button
    type="button"
    role="switch"
    aria-checked={on}
    onClick={() => onChange(!on)}
    className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition ${
      on ? "bg-primary" : "bg-muted"
    }`}
  >
    <span
      className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition ${
        on ? "translate-x-4" : "translate-x-0.5"
      }`}
    />
  </button>
);

// ============================================================================
// Page
// ============================================================================
export const ConnectionsPage = () => {
  const { t } = useTranslation();
  const sessions = useSessions((s) => s.sessions);
  const [creating, setCreating] = useState(false);
  const [toDelete, setToDelete] = useState<SessionInfo | null>(null);
  const [qrFor, setQrFor] = useState<SessionInfo | null>(null);
  const [disconnectFor, setDisconnectFor] = useState<SessionInfo | null>(null);
  const [payFor, setPayFor] = useState<SessionInfo | null>(null);
  const [editFor, setEditFor] = useState<SessionInfo | null>(null);

  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<"all" | "connected" | "disconnected">("all");
  const [alertModalOpen, setAlertModalOpen] = useState(false);
  const [alertsEnabled, setAlertsEnabled] = useState(false);

  const connectedCount = sessions.filter((s) => s.paired && s.state === "open").length;
  const disconnectedCount = sessions.length - connectedCount;

  const filteredSessions = sessions.filter((s) => {
    const isConnected = s.paired && s.state === "open";
    if (statusFilter === "connected" && !isConnected) return false;
    if (statusFilter === "disconnected" && isConnected) return false;
    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase().trim();
      const nameMatch = s.name ? s.name.toLowerCase().includes(q) : false;
      const jidMatch = s.jid ? s.jid.toLowerCase().includes(q) : false;
      return nameMatch || jidMatch;
    }
    return true;
  });

  useEffect(() => {
    ensureSessionsWired();
    getEvolutionAlert()
      .then((cfg) => setAlertsEnabled(Boolean(cfg?.enabled)))
      .catch(() => {});
  }, []);

  return (
    <AppShell>
      <div className="space-y-5 pb-12">
        {/* Header */}
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold tracking-tight">{t("pages.connections.title")}</h2>
            <p className="text-sm text-muted-foreground">{t("pages.connections.subtitle")}</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="outline"
              onClick={() => setAlertModalOpen(true)}
              className="gap-2 border-border/80"
              title="Configurar Notificações via Evolution API"
            >
              <Bell className="h-4 w-4 text-muted-foreground" />
              <span>Mensagem API</span>
              {alertsEnabled && (
                <span className="relative flex h-2 w-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                  <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
                </span>
              )}
            </Button>
            <Button onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" /> {t("pages.connections.newInstance")}
            </Button>
          </div>
        </div>

        {/* Filters & Search bar */}
        {sessions.length > 0 && (
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-1">
            {/* Status pills */}
            <div className="flex flex-wrap items-center gap-1.5 rounded-lg border bg-muted/30 p-1">
              <button
                type="button"
                onClick={() => setStatusFilter("all")}
                className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition ${
                  statusFilter === "all"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                Todos
                <span
                  className={`ml-0.5 rounded-full px-1.5 py-0.2 text-[10px] ${
                    statusFilter === "all" ? "bg-muted font-bold text-foreground" : "bg-muted/70 text-muted-foreground"
                  }`}
                >
                  {sessions.length}
                </span>
              </button>

              <button
                type="button"
                onClick={() => setStatusFilter("connected")}
                className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition ${
                  statusFilter === "connected"
                    ? "bg-background text-emerald-500 shadow-sm font-semibold"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <span className="h-2 w-2 rounded-full bg-emerald-500" />
                Conectados
                <span className="ml-0.5 rounded-full bg-emerald-500/15 px-1.5 py-0.2 text-[10px] text-emerald-500 font-bold">
                  {connectedCount}
                </span>
              </button>

              <button
                type="button"
                onClick={() => setStatusFilter("disconnected")}
                className={`inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition ${
                  statusFilter === "disconnected"
                    ? "bg-background text-rose-400 shadow-sm font-semibold"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                <span className="h-2 w-2 rounded-full bg-muted-foreground/50" />
                Desconectados
                <span className="ml-0.5 rounded-full bg-muted px-1.5 py-0.2 text-[10px] text-muted-foreground font-bold">
                  {disconnectedCount}
                </span>
              </button>
            </div>

            {/* Search Input */}
            <div className="relative w-full sm:w-64">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground" />
              <Input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Buscar por nome ou número..."
                className="h-8 pl-8 pr-8 text-xs"
              />
              {searchQuery && (
                <button
                  type="button"
                  onClick={() => setSearchQuery("")}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  aria-label="Limpar busca"
                >
                  <X className="h-3 w-3" />
                </button>
              )}
            </div>
          </div>
        )}

        {sessions.length === 0 ? (
          <div className="grid place-items-center rounded-xl border border-dashed bg-card/40 p-12 text-center">
            <div className="grid h-12 w-12 place-items-center rounded-full bg-muted text-muted-foreground">
              <Smartphone className="h-5 w-5" />
            </div>
            <div className="mt-3 text-sm font-medium">{t("pages.connections.emptyTitle")}</div>
            <div className="mt-1 text-xs text-muted-foreground">{t("pages.connections.emptyDescription")}</div>
            <Button className="mt-4" onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" /> {t("pages.connections.newInstance")}
            </Button>
          </div>
        ) : filteredSessions.length === 0 ? (
          <div className="grid place-items-center rounded-xl border border-dashed bg-card/40 p-10 text-center">
            <WifiOff className="h-8 w-8 text-muted-foreground" />
            <div className="mt-2 text-sm font-medium">Nenhuma conexão encontrada</div>
            <div className="mt-1 text-xs text-muted-foreground">
              Nenhuma conexão corresponde aos filtros aplicados.
            </div>
            <Button
              variant="outline"
              size="sm"
              className="mt-3"
              onClick={() => {
                setStatusFilter("all");
                setSearchQuery("");
              }}
            >
              Limpar filtros
            </Button>
          </div>
        ) : (
          <div className="space-y-3">
            {filteredSessions.map((s) => (
              <InstanceRow
                key={s.id}
                s={s}
                onConfigure={() => setEditFor(s)}
                onConnect={() => setQrFor(s)}
                onDisconnect={() => setDisconnectFor(s)}
                onPay={() => setPayFor(s)}
                onRestart={() =>
                  pairSession(s.id)
                    .then(() => toast.success(t("pages.connections.restartingToast")))
                    .catch((e) => toast.error((e as Error).message))
                }
                onDelete={() => setToDelete(s)}
              />
            ))}
          </div>
        )}
      </div>

      <CreateConnectionModal
        open={creating}
        onOpenChange={setCreating}
        onCreated={(id) => {
          const found = useSessions.getState().sessions.find((x) => x.id === id);
          if (found) setEditFor(found);
        }}
      />
      {qrFor && (
        <QRDialog
          open={!!qrFor}
          onOpenChange={(o) => !o && setQrFor(null)}
          sessionId={qrFor.id}
          sessionName={qrFor.name}
        />
      )}
      {disconnectFor && (
        <DisconnectDialog
          open={!!disconnectFor}
          onOpenChange={(o) => !o && setDisconnectFor(null)}
          sessionId={disconnectFor.id}
          sessionName={disconnectFor.name}
        />
      )}
      {payFor && (
        <PaymentDialog
          open={!!payFor}
          onOpenChange={(o) => !o && setPayFor(null)}
          sessionId={payFor.id}
          sessionName={payFor.name}
          price={getInstancePlan(payFor.id).price || 49.9}
        />
      )}
      {editFor && (
        <EditConnectionModal
          open={!!editFor}
          onOpenChange={(o) => !o && setEditFor(null)}
          session={editFor}
        />
      )}
      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title={t("pages.connections.deleteTitle")}
        description={toDelete ? t("pages.connections.deleteDescription", { name: toDelete.name }) : undefined}
        confirmLabel={t("common.delete")}
        destructive
        onConfirm={() => {
          if (toDelete) {
            void deleteSession(toDelete.id).catch((e) => toast.error((e as Error).message));
          }
        }}
      />
      <EvolutionAlertModal
        open={alertModalOpen}
        onOpenChange={setAlertModalOpen}
        onUpdated={(enabled) => setAlertsEnabled(enabled)}
      />

      {/* Helpful footer link to the dedicated chat workspace */}
      <div className="pb-4 text-center text-[11px] text-muted-foreground">
        <Link to="/chats" className="hover:text-foreground">
          {t("actions.openInbox")} →
        </Link>
      </div>
    </AppShell>
  );
};
