import { useState } from "react";
import { AlertTriangle, CheckCircle2, Loader2, RefreshCw, ShieldAlert } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { reconcileSupportRequest, SupportApiError } from "@/services/support";
import type { ReconcilePayload, SupportTicketResponseEnvelope } from "@/types/support";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  requestId: string;
  onSuccess: (envelope: SupportTicketResponseEnvelope) => void;
}

export const ReconcileDialog = ({
  open,
  onOpenChange,
  requestId,
  onSuccess,
}: Props) => {
  const [outcome, setOutcome] = useState<ReconcilePayload["outcome"]>("synced");
  const [glpiTicketId, setGlpiTicketId] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;

    if (outcome === "synced") {
      const cleanId = glpiTicketId.trim();
      if (!cleanId || !/^[1-9]\d*$/.test(cleanId)) {
        setErrorMsg("Informe o ID numérico decimal positivo do ticket GLPI.");
        return;
      }
    }

    setSubmitting(true);
    setErrorMsg(null);

    try {
      const payload: ReconcilePayload = {
        outcome,
        glpiTicketId: outcome === "synced" ? glpiTicketId.trim() : undefined,
      };
      const response = await reconcileSupportRequest(requestId, payload);
      onSuccess(response);
      onOpenChange(false);
    } catch (err: any) {
      if (err instanceof SupportApiError) {
        if (err.code === "ticket_not_found") {
          setErrorMsg("Ticket GLPI não foi encontrado com o ID informado.");
        } else if (err.code === "reconcile_external_id_mismatch") {
          setErrorMsg("O ticket no GLPI existe mas não corresponde ao external_id deste atendimento.");
        } else {
          setErrorMsg(err.message || `Erro ${err.status}: falha ao reconciliar.`);
        }
      } else {
        setErrorMsg(err?.message || "Falha na comunicação ao reconciliar.");
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base font-semibold text-orange-600 dark:text-orange-400">
            <ShieldAlert className="h-5 w-5" />
            Conciliação Administrativa (GLPI)
          </DialogTitle>
          <DialogDescription className="text-xs text-muted-foreground">
            Ação restrita a administradores. Verifique previamente no console do GLPI se o chamado foi gerado antes de definir o desfecho.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-2">
          <div className="rounded-md border bg-muted/30 p-2.5 text-xs text-muted-foreground">
            <span className="font-mono text-[11px] block truncate">
              Request ID: {requestId}
            </span>
          </div>

          <div className="space-y-2.5 text-xs">
            <label className="font-semibold text-foreground block">
              Desfecho verificado no GLPI:
            </label>

            <label className="flex items-start gap-2.5 p-2 rounded border cursor-pointer hover:bg-muted/40 transition">
              <input
                type="radio"
                name="outcome"
                value="synced"
                checked={outcome === "synced"}
                onChange={() => setOutcome("synced")}
                className="mt-0.5"
                disabled={submitting}
              />
              <div className="flex-1">
                <div className="font-medium text-foreground flex items-center gap-1.5">
                  <CheckCircle2 className="h-3.5 w-3.5 text-emerald-600" />
                  Chamado FOI criado no GLPI
                </div>
                <p className="text-[11px] text-muted-foreground mt-0.5">
                  Associa o ticket existente e transita para status sincronizado (synced).
                </p>
                {outcome === "synced" && (
                  <div className="mt-2 space-y-1">
                    <label htmlFor="reconcile-ticket-id" className="text-[11px] font-medium text-foreground block">
                      ID do Ticket no GLPI *
                    </label>
                    <Input
                      id="reconcile-ticket-id"
                      value={glpiTicketId}
                      onChange={(e) => setGlpiTicketId(e.target.value.replace(/\D/g, ""))}
                      placeholder="Ex: 14892"
                      required
                      disabled={submitting}
                      className="text-xs font-mono"
                      autoFocus
                    />
                  </div>
                )}
              </div>
            </label>

            <label className="flex items-start gap-2.5 p-2 rounded border cursor-pointer hover:bg-muted/40 transition">
              <input
                type="radio"
                name="outcome"
                value="safe_to_retry"
                checked={outcome === "safe_to_retry"}
                onChange={() => setOutcome("safe_to_retry")}
                className="mt-0.5"
                disabled={submitting}
              />
              <div className="flex-1">
                <div className="font-medium text-foreground flex items-center gap-1.5">
                  <RefreshCw className="h-3.5 w-3.5 text-amber-600" />
                  Chamado NÃO foi criado no GLPI
                </div>
                <p className="text-[11px] text-muted-foreground mt-0.5">
                  Libera o chamado com segurança para nova tentativa manual pelo atendente (retryable_error).
                </p>
              </div>
            </label>
          </div>

          {errorMsg && (
            <div className="rounded-md border border-destructive/30 bg-destructive/10 p-2.5 text-xs text-destructive flex items-start gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
              <div className="flex-1 font-medium">{errorMsg}</div>
            </div>
          )}

          <DialogFooter className="gap-2 sm:gap-0 pt-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
              disabled={submitting}
            >
              Cancelar
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={submitting}
              className="bg-orange-600 hover:bg-orange-700 text-white"
            >
              {submitting ? (
                <>
                  <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                  Conciliando...
                </>
              ) : (
                "Executar Conciliação"
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
};
