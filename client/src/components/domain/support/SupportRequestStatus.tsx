import { useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  Check,
  CheckCircle2,
  Clock,
  Copy,
  ExternalLink,
  HelpCircle,
  Loader2,
  Plus,
  RefreshCw,
  ShieldAlert,
  Ticket,
  XCircle,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DeviceSummary } from "./DeviceSummary";
import { RetryAction } from "./RetryAction";
import { ReconcileDialog } from "./ReconcileDialog";
import { isAdmin } from "@/stores/auth";
import { sanitizeGLPIWebUrl } from "@/lib/supportUrl";
import { formatUnixSeconds } from "@/lib/supportDate";
import type { AuthUser } from "@/types/auth";
import type { DeviceBindingDTO, SupportRequestDTO, SupportSyncState, SupportTicketResponseEnvelope } from "@/types/support";

interface Props {
  request: SupportRequestDTO;
  device?: DeviceBindingDTO | null;
  currentUser: AuthUser | null;
  onUpdate: (envelope: SupportTicketResponseEnvelope) => void;
  onNewTicketClick?: () => void;
  onChangeDeviceClick?: () => void;
}

const STATE_CONFIG: Record<
  SupportSyncState,
  {
    label: string;
    badgeCls: string;
    bannerCls: string;
    icon: typeof CheckCircle2;
    description: string;
  }
> = {
  processing: {
    label: "Sincronizando",
    badgeCls: "bg-blue-500/15 text-blue-600 dark:text-blue-400 border-blue-500/30",
    bannerCls: "bg-blue-500/10 border-blue-500/30 text-blue-900 dark:text-blue-300",
    icon: Loader2,
    description: "Enviando requisição segura ao GLPI. Aguarde...",
  },
  synced: {
    label: "Sincronizado",
    badgeCls: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30",
    bannerCls: "bg-emerald-500/10 border-emerald-500/30 text-emerald-900 dark:text-emerald-300",
    icon: CheckCircle2,
    description: "Chamado aberto e confirmado com sucesso no GLPI.",
  },
  retryable_error: {
    label: "Falha Recuperável",
    badgeCls: "bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30",
    bannerCls: "bg-amber-500/10 border-amber-500/30 text-amber-900 dark:text-amber-300",
    icon: AlertTriangle,
    description: "Instabilidade temporária na integração. Nova tentativa disponível.",
  },
  unknown: {
    label: "Confirmação Pendente",
    badgeCls: "bg-orange-500/15 text-orange-600 dark:text-orange-400 border-orange-500/30",
    bannerCls: "bg-orange-500/10 border-orange-500/30 text-orange-900 dark:text-orange-300",
    icon: HelpCircle,
    description: "Timeout/transporte durante envio ao GLPI. NÃO reenvie para evitar duplicidade.",
  },
  failed: {
    label: "Falha Permanente",
    badgeCls: "bg-red-500/15 text-red-600 dark:text-red-400 border-red-500/30",
    bannerCls: "bg-red-500/10 border-red-500/30 text-red-900 dark:text-red-300",
    icon: XCircle,
    description: "Solicitação rejeitada pelo GLPI. Corrija os dados e gere um novo chamado.",
  },
};

const PRIORITY_LABELS: Record<number, string> = {
  1: "Muito Baixa",
  2: "Baixa",
  3: "Média",
  4: "Alta",
  5: "Muito Alta",
};

export const SupportRequestStatus = ({
  request,
  device,
  currentUser,
  onUpdate,
  onNewTicketClick,
  onChangeDeviceClick,
}: Props) => {
  const [copiedNumber, setCopiedNumber] = useState(false);
  const [showReconcile, setShowReconcile] = useState(false);
  const [showFullDesc, setShowFullDesc] = useState(false);

  const stateCfg = STATE_CONFIG[request.syncState] || STATE_CONFIG.unknown;
  const StateIcon = stateCfg.icon;

  const handleCopyNumber = async () => {
    if (!request.glpiTicketId) return;
    try {
      await navigator.clipboard.writeText(request.glpiTicketId);
      setCopiedNumber(true);
      setTimeout(() => setCopiedNumber(false), 2000);
    } catch {
      // ignore
    }
  };

  const safeWebUrl = sanitizeGLPIWebUrl(request.webUrl);
  const userIsAdmin = isAdmin(currentUser);
  const formattedDate = formatUnixSeconds(request.createdAt);

  return (
    <div className="space-y-4">
      {/* State Banner */}
      <div className={`rounded-lg border p-3.5 text-xs ${stateCfg.bannerCls}`}>
        <div className="flex items-start gap-2.5">
          <StateIcon className={`h-4 w-4 shrink-0 mt-0.5 ${request.syncState === "processing" ? "animate-spin" : ""}`} />
          <div className="flex-1 min-w-0">
            <div className="flex items-center justify-between gap-2">
              <span className="font-semibold text-sm">Status: {stateCfg.label}</span>
              <Badge variant="outline" className={`text-[10px] ${stateCfg.badgeCls}`}>
                {request.syncState}
              </Badge>
            </div>
            <p className="mt-1 font-medium">{stateCfg.description}</p>
            {request.lastErrorCode && (
              <p className="mt-1 text-[11px] opacity-85 font-mono">
                Código de retorno: {request.lastErrorCode}
              </p>
            )}
          </div>
        </div>
      </div>

      {/* GLPI Ticket Confirmation Box */}
      {request.syncState === "synced" && request.glpiTicketId && (
        <div className="rounded-lg border bg-emerald-500/5 border-emerald-500/30 p-3.5 space-y-2.5">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1.5 font-semibold text-xs text-foreground">
              <Ticket className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              <span>Ticket GLPI Confirmado</span>
            </div>
            <span className="font-mono font-bold text-emerald-700 dark:text-emerald-400 text-sm">
              #{request.glpiTicketId}
            </span>
          </div>

          <div className="flex items-center gap-2 pt-1">
            {safeWebUrl && (
              <Button
                asChild
                size="sm"
                variant="default"
                className="h-8 text-xs bg-emerald-600 hover:bg-emerald-700 text-white"
              >
                <a
                  href={safeWebUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-1.5"
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                  Abrir no GLPI
                </a>
              </Button>
            )}
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={handleCopyNumber}
              className="h-8 text-xs"
            >
              {copiedNumber ? (
                <>
                  <Check className="mr-1 h-3.5 w-3.5 text-emerald-500" />
                  Copiado!
                </>
              ) : (
                <>
                  <Copy className="mr-1 h-3.5 w-3.5" />
                  Copiar número
                </>
              )}
            </Button>
          </div>
        </div>
      )}

      {/* Retryable Error Action */}
      {request.syncState === "retryable_error" && (
        <div className="rounded-lg border border-amber-500/30 bg-card p-3 space-y-2">
          <span className="text-xs font-semibold text-foreground block">
            Ação Recomendada
          </span>
          <RetryAction
            requestId={request.id}
            onSuccess={onUpdate}
            // request.updatedAt is Unix SECONDS (cmd/server: time.Now().UTC().Unix()),
            // not milliseconds — convert before arithmetic against Date.now().
            retryAfterUntil={request.lastErrorCode === "rate_limited" ? request.updatedAt * 1000 + 60_000 : undefined}
          />
        </div>
      )}

      {/* Unknown State: User guidance vs Admin reconcile */}
      {request.syncState === "unknown" && (
        <div className="rounded-lg border border-orange-500/30 bg-card p-3 space-y-2.5">
          <div className="flex items-start gap-2 text-xs text-orange-900 dark:text-orange-300">
            <AlertCircle className="h-4 w-4 shrink-0 text-orange-600 mt-0.5" />
            <div>
              <span className="font-semibold block">Orientações de Segurança</span>
              <p className="mt-0.5 text-muted-foreground">
                A requisição pode ter sido recebida e processada pelo GLPI antes da desconexão. Verifique antes de reenviar para não gerar chamados duplicados.
              </p>
            </div>
          </div>

          {userIsAdmin ? (
            <div className="pt-2 border-t">
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => setShowReconcile(true)}
                className="w-full text-xs text-orange-600 border-orange-500/40 hover:bg-orange-500/10"
              >
                <ShieldAlert className="mr-1.5 h-3.5 w-3.5" />
                Conciliar Chamado (Admin)
              </Button>
            </div>
          ) : (
            <p className="text-[11px] text-muted-foreground italic border-t pt-1.5">
              Operador: solicite a conciliação manual a um supervisor administrador caso necessário.
            </p>
          )}
        </div>
      )}

      {/* Permanent Failure Action */}
      {request.syncState === "failed" && onNewTicketClick && (
        <div className="rounded-lg border border-red-500/30 bg-card p-3 space-y-2">
          <span className="text-xs font-semibold text-foreground block">
            Ação Necessária
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={onNewTicketClick}
            className="w-full text-xs"
          >
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            Criar Novo Chamado
          </Button>
        </div>
      )}

      {/* Linked Equipment */}
      <div className="space-y-1.5">
        <span className="text-xs font-semibold text-foreground block">
          Computador Vinculado
        </span>
        <DeviceSummary
          device={device}
          hostnameInformed={request.hostnameInformed}
          onChangeDeviceClick={request.syncState !== "processing" ? onChangeDeviceClick : undefined}
          disabled={request.syncState === "processing"}
        />
      </div>

      {/* Ticket Details Summary Card */}
      <div className="rounded-lg border bg-card p-3.5 text-xs text-card-foreground shadow-xs space-y-2.5">
        <div className="flex items-center justify-between border-b pb-2">
          <span className="font-semibold text-foreground">Dados do Chamado</span>
          <span className="font-mono text-[10px] text-muted-foreground" title={request.id}>
            ID: {request.id.slice(0, 12)}...
          </span>
        </div>

        <div className="grid grid-cols-2 gap-2 text-muted-foreground">
          <div className="col-span-2">
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Título
            </span>
            <span className="text-foreground font-medium block">{request.title}</span>
          </div>

          <div>
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Solicitante
            </span>
            <span className="text-foreground font-medium truncate block">
              {request.requesterName}
            </span>
          </div>

          <div>
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Prioridade
            </span>
            <span className="text-foreground font-medium block">
              {PRIORITY_LABELS[request.priority] || `Grau ${request.priority}`}
            </span>
          </div>

          <div className="col-span-2">
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Descrição
            </span>
            <p className={`text-foreground whitespace-pre-wrap mt-0.5 ${showFullDesc ? "" : "line-clamp-3"}`}>
              {request.description}
            </p>
            {request.description.length > 180 && (
              <button
                type="button"
                onClick={() => setShowFullDesc((v) => !v)}
                className="text-[11px] text-primary hover:underline mt-1 block"
              >
                {showFullDesc ? "Recolher descrição" : "Ver descrição completa"}
              </button>
            )}
          </div>

          <div className="col-span-2 flex items-center justify-between pt-1 border-t text-[11px]">
            <span>Criado em: {formattedDate}</span>
            <span>Tentativas: {request.attemptCount}</span>
          </div>
        </div>
      </div>

      {/* Admin Reconcile Dialog Modal */}
      {showReconcile && (
        <ReconcileDialog
          open={showReconcile}
          onOpenChange={setShowReconcile}
          requestId={request.id}
          onSuccess={onUpdate}
        />
      )}
    </div>
  );
};
