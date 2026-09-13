import { useState } from "react";
import { Check, Copy, Laptop, Monitor, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import type { DeviceBindingDTO, DeviceMatchStatus } from "@/types/support";

interface Props {
  device?: DeviceBindingDTO | null;
  hostnameInformed?: string;
  onChangeDeviceClick?: () => void;
  disabled?: boolean;
}

const MATCH_BADGES: Record<DeviceMatchStatus, { label: string; cls: string }> = {
  matched: { label: "Vinculado", cls: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30" },
  pending: { label: "Pendente", cls: "bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30" },
  conflict: { label: "Conflito", cls: "bg-red-500/15 text-red-600 dark:text-red-400 border-red-500/30" },
  missing_glpi: { label: "Sem GLPI", cls: "bg-orange-500/15 text-orange-600 dark:text-orange-400 border-orange-500/30" },
  missing_tactical: { label: "Sem Tactical", cls: "bg-sky-500/15 text-sky-600 dark:text-sky-400 border-sky-500/30" },
  disabled: { label: "Desativado", cls: "bg-muted text-muted-foreground border-border" },
};

export const DeviceSummary = ({
  device,
  hostnameInformed,
  onChangeDeviceClick,
  disabled = false,
}: Props) => {
  const [copied, setCopied] = useState(false);

  const displayHostname = device?.hostname || hostnameInformed;

  const handleCopyHostname = async () => {
    if (!displayHostname) return;
    try {
      await navigator.clipboard.writeText(displayHostname);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // ignore clipboard error
    }
  };

  const matchInfo = device?.matchStatus ? MATCH_BADGES[device.matchStatus] : undefined;

  if (!device && !hostnameInformed) {
    return (
      <div className="rounded-lg border border-dashed p-4 text-center">
        <Monitor className="mx-auto h-8 w-8 text-muted-foreground/60 mb-2" />
        <p className="text-xs font-medium text-muted-foreground">
          Nenhum computador vinculado a este contato.
        </p>
        {onChangeDeviceClick && (
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="mt-3 text-xs"
            onClick={onChangeDeviceClick}
            disabled={disabled}
          >
            <Laptop className="mr-1.5 h-3.5 w-3.5" />
            Localizar e Vincular Computador
          </Button>
        )}
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-card p-3.5 text-card-foreground shadow-xs">
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <Monitor className="h-4 w-4 shrink-0 text-primary" />
          <span className="truncate font-semibold text-sm" title={displayHostname}>
            {displayHostname || "Desconhecido"}
          </span>
          {displayHostname && (
            <button
              type="button"
              onClick={handleCopyHostname}
              title={copied ? "Copiado!" : "Copiar hostname"}
              className="grid h-6 w-6 place-items-center rounded text-muted-foreground hover:bg-muted focus:outline-hidden"
            >
              {copied ? (
                <Check className="h-3 w-3 text-emerald-500" />
              ) : (
                <Copy className="h-3 w-3" />
              )}
            </button>
          )}
        </div>
        {onChangeDeviceClick && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="h-7 px-2 text-xs"
            onClick={onChangeDeviceClick}
            disabled={disabled}
          >
            <RefreshCw className="mr-1 h-3 w-3" />
            Trocar
          </Button>
        )}
      </div>

      <div className="mt-2.5 grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs text-muted-foreground">
        {device?.sectorCode && (
          <div>
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Setor
            </span>
            <span className="text-foreground font-medium truncate block">
              {device.sectorCode}
            </span>
          </div>
        )}
        {device?.patrimonio && (
          <div>
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block">
              Patrimônio
            </span>
            <span className="text-foreground font-medium truncate block">
              {device.patrimonio}
            </span>
          </div>
        )}
        {matchInfo && (
          <div className="col-span-2 mt-1">
            <span className="text-[10px] uppercase font-medium text-muted-foreground/70 block mb-0.5">
              Status de Vínculo
            </span>
            <Badge variant="outline" className={`text-[10px] px-1.5 py-0 ${matchInfo.cls}`}>
              {matchInfo.label}
            </Badge>
          </div>
        )}
      </div>
    </div>
  );
};
