import { Activity, AlertTriangle, CheckCircle, Clock, Globe, Laptop, ShieldAlert } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { TacticalAgentSummary } from "@/types/support";

interface Props {
  tacticalEnabled: boolean;
  tacticalAgent?: TacticalAgentSummary | null;
  warnings?: string[];
}

export const TacticalStatus = ({
  tacticalEnabled,
  tacticalAgent,
  warnings = [],
}: Props) => {
  // If Tactical is not enabled at system level, suppress telemetry completely (Fallback T-006)
  if (!tacticalEnabled) {
    return null;
  }

  const isUnavailable = warnings.includes("tactical_unavailable");

  if (isUnavailable) {
    return (
      <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-800 dark:text-amber-300">
        <div className="flex items-start gap-2">
          <AlertTriangle className="h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400 mt-0.5" />
          <div>
            <span className="font-semibold block">Telemetria Tactical RMM Indisponível</span>
            <p className="mt-0.5 text-muted-foreground dark:text-amber-200/80">
              Não foi possível consultar os dados em tempo real no momento. A criação e o vínculo do chamado prosseguem normalmente.
            </p>
          </div>
        </div>
      </div>
    );
  }

  if (!tacticalAgent) {
    return (
      <div className="rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
        <div className="flex items-center gap-2">
          <Activity className="h-3.5 w-3.5 text-muted-foreground/70" />
          <span>Nenhum agente Tactical vinculado a este equipamento.</span>
        </div>
      </div>
    );
  }

  const status = (tacticalAgent.status || "").toLowerCase();
  let statusBadge = (
    <Badge variant="outline" className="text-[10px] bg-muted text-muted-foreground border-border">
      Offline
    </Badge>
  );

  if (status === "online") {
    statusBadge = (
      <Badge variant="outline" className="text-[10px] bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30 flex items-center gap-1">
        <span className="h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse" />
        Online
      </Badge>
    );
  } else if (status === "overdue") {
    statusBadge = (
      <Badge variant="outline" className="text-[10px] bg-red-500/15 text-red-600 dark:text-red-400 border-red-500/30 flex items-center gap-1">
        <ShieldAlert className="h-2.5 w-2.5" />
        Atrasado (Overdue)
      </Badge>
    );
  }

  const os = tacticalAgent.operatingSystem || tacticalAgent.operating_system;
  const ip = tacticalAgent.publicIp || tacticalAgent.public_ip;
  const lastSeen = tacticalAgent.lastSeen || tacticalAgent.last_seen;
  const user = tacticalAgent.loggedUsername || tacticalAgent.logged_username;

  return (
    <div className="rounded-lg border bg-card p-3.5 text-card-foreground shadow-xs">
      <div className="flex items-center justify-between gap-2 border-b pb-2 mb-2">
        <div className="flex items-center gap-1.5 font-semibold text-xs text-foreground">
          <Activity className="h-3.5 w-3.5 text-primary" />
          <span>Tactical RMM Telemetria</span>
        </div>
        {statusBadge}
      </div>

      <div className="grid grid-cols-2 gap-2 text-xs">
        {os && (
          <div className="col-span-2">
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Sistema Operacional
            </span>
            <span className="text-foreground font-medium truncate block">{os}</span>
          </div>
        )}

        {ip && (
          <div>
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              IP Conexão
            </span>
            <span className="text-foreground font-mono text-[11px] truncate block">{ip}</span>
          </div>
        )}

        {user && (
          <div>
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Usuário Ativo
            </span>
            <span className="text-foreground font-medium truncate block">{user}</span>
          </div>
        )}

        {lastSeen && (
          <div className="col-span-2 text-[11px] text-muted-foreground flex items-center gap-1 mt-0.5">
            <Clock className="h-3 w-3" />
            <span>Último check-in: {new Date(lastSeen).toLocaleString()}</span>
          </div>
        )}
      </div>
    </div>
  );
};
