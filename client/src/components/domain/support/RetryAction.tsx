import { useEffect, useState } from "react";
import { AlertTriangle, Loader2, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { retrySupportRequest, SupportApiError } from "@/services/support";
import type { SupportTicketResponseEnvelope } from "@/types/support";

interface Props {
  requestId: string;
  onSuccess: (envelope: SupportTicketResponseEnvelope) => void;
  disabled?: boolean;
}

export const RetryAction = ({ requestId, onSuccess, disabled = false }: Props) => {
  const [retrying, setRetrying] = useState(false);
  const [countdown, setCountdown] = useState<number>(0);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    if (countdown <= 0) return;
    const interval = setInterval(() => {
      setCountdown((c) => (c > 1 ? c - 1 : 0));
    }, 1000);
    return () => clearInterval(interval);
  }, [countdown]);

  const handleRetry = async () => {
    if (retrying || countdown > 0 || disabled) return;
    setRetrying(true);
    setErrorMsg(null);

    try {
      const response = await retrySupportRequest(requestId);
      onSuccess(response);
    } catch (err: any) {
      if (err instanceof SupportApiError) {
        if (err.status === 429 && err.retryAfterSeconds) {
          setCountdown(err.retryAfterSeconds);
          setErrorMsg(`Muitas requisições. Aguarde ${err.retryAfterSeconds}s para tentar novamente.`);
        } else {
          setErrorMsg(err.message || `Erro ${err.status}: falha no retry.`);
        }
      } else {
        setErrorMsg(err?.message || "Falha na comunicação ao tentar novamente.");
      }
    } finally {
      setRetrying(false);
    }
  };

  return (
    <div className="space-y-1.5">
      <Button
        type="button"
        size="sm"
        variant="default"
        onClick={handleRetry}
        disabled={disabled || retrying || countdown > 0}
        className="w-full bg-amber-600 hover:bg-amber-700 text-white dark:bg-amber-700 dark:hover:bg-amber-800 text-xs"
      >
        {retrying ? (
          <>
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
            Tentando Sincronizar...
          </>
        ) : countdown > 0 ? (
          `Aguarde ${countdown}s antes de tentar`
        ) : (
          <>
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            Repetir Criação (Retry)
          </>
        )}
      </Button>

      {errorMsg && (
        <div className="flex items-center gap-1.5 text-[11px] text-destructive font-medium">
          <AlertTriangle className="h-3 w-3 shrink-0" />
          <span>{errorMsg}</span>
        </div>
      )}
    </div>
  );
};
