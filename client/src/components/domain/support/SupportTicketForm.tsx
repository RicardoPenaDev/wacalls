import { useEffect, useRef, useState } from "react";
import { AlertTriangle, ChevronDown, ChevronUp, Loader2, Send, Ticket } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { DeviceSummary } from "./DeviceSummary";
import { createChatSupportTicket, SupportApiError } from "@/services/support";
import type { DeviceBindingDTO, SupportTicketResponseEnvelope } from "@/types/support";

interface Props {
  sessionId: string;
  chatJid: string;
  defaultRequesterName: string;
  selectedDevice?: DeviceBindingDTO | null;
  onChangeDeviceClick?: () => void;
  onSuccess: (envelope: SupportTicketResponseEnvelope) => void;
  onCancel?: () => void;
}

export const generateIdempotencyKey = (): string => {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return `ui-${crypto.randomUUID()}`;
  }
  // Safe fallback for environments without crypto.randomUUID
  const rand = Math.random().toString(36).substring(2, 15);
  const time = Date.now().toString(36);
  return `ui-${rand}-${time}-${Math.random().toString(36).substring(2, 10)}`;
};

export const SupportTicketForm = ({
  sessionId,
  chatJid,
  defaultRequesterName,
  selectedDevice,
  onChangeDeviceClick,
  onSuccess,
  onCancel,
}: Props) => {
  const [requesterName, setRequesterName] = useState(defaultRequesterName || "");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState<number>(3); // 3 - Média
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [categoryId, setCategoryId] = useState("");
  const [locationId, setLocationId] = useState("");

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [retryCountdown, setRetryCountdown] = useState<number>(0);

  // Preserve stable Idempotency-Key across component re-renders
  const idempotencyKeyRef = useRef<string>(generateIdempotencyKey());

  // Countdown timer for HTTP 429 rate limit
  useEffect(() => {
    if (retryCountdown <= 0) return;
    const interval = setInterval(() => {
      setRetryCountdown((prev) => (prev > 1 ? prev - 1 : 0));
    }, 1000);
    return () => clearInterval(interval);
  }, [retryCountdown]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (isSubmitting || retryCountdown > 0) return;

    // Client-side validations
    const reqTrim = requesterName.trim();
    if (!reqTrim || reqTrim.length > 120) {
      setErrorMessage("O nome do solicitante é obrigatório (máximo 120 caracteres).");
      return;
    }

    const titleTrim = title.trim();
    if (!titleTrim || titleTrim.length > 200) {
      setErrorMessage("O título é obrigatório (1 a 200 caracteres).");
      return;
    }

    const descTrim = description.trim();
    if (!descTrim || descTrim.length > 8000) {
      setErrorMessage("A descrição é obrigatória (1 a 8000 caracteres).");
      return;
    }

    setIsSubmitting(true);
    setErrorMessage(null);

    try {
      const payload = {
        requesterName: reqTrim,
        title: titleTrim,
        description: descTrim,
        deviceBindingId: selectedDevice?.id,
        hostname: selectedDevice?.hostname,
        categoryId: categoryId.trim() || undefined,
        locationId: locationId.trim() || undefined,
        priority,
      };

      const response = await createChatSupportTicket(
        sessionId,
        chatJid,
        idempotencyKeyRef.current,
        payload,
      );

      // Successfully processed (synced, retryable_error, unknown, or processing)
      onSuccess(response);
    } catch (err: any) {
      if (err instanceof SupportApiError) {
        if (err.status === 429 && err.retryAfterSeconds) {
          setRetryCountdown(err.retryAfterSeconds);
          setErrorMessage(
            `Limite de requisições excedido. Aguarde ${err.retryAfterSeconds} segundos antes de reenviar.`,
          );
        } else if (err.status === 503) {
          setErrorMessage("O módulo de suporte está desabilitado no momento.");
        } else if (err.status === 409) {
          setErrorMessage(
            "Conflito de envio detectado. Recarregue os dados da conversa e tente novamente.",
          );
          // On 409 idempotency conflict, regenerate key for next attempt
          idempotencyKeyRef.current = generateIdempotencyKey();
        } else {
          setErrorMessage(err.message || `Erro ${err.status}: falha ao abrir chamado.`);
        }
      } else {
        setErrorMessage(err?.message || "Erro de rede ao submeter chamado. Rascunho mantido.");
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const remainingChars = 8000 - description.length;

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-1.5">
        <span className="text-xs font-semibold text-foreground block">
          Computador Vinculado
        </span>
        <DeviceSummary
          device={selectedDevice}
          onChangeDeviceClick={onChangeDeviceClick}
          disabled={isSubmitting}
        />
      </div>

      <div className="space-y-1">
        <label htmlFor="support-requester" className="text-xs font-semibold text-foreground block">
          Solicitante <span className="text-destructive">*</span>
        </label>
        <Input
          id="support-requester"
          value={requesterName}
          onChange={(e) => setRequesterName(e.target.value)}
          placeholder="Nome completo do solicitante"
          maxLength={120}
          required
          disabled={isSubmitting}
          className="text-xs"
        />
      </div>

      <div className="space-y-1">
        <div className="flex items-center justify-between">
          <label htmlFor="support-title" className="text-xs font-semibold text-foreground block">
            Título do Chamado <span className="text-destructive">*</span>
          </label>
          <span className="text-[10px] text-muted-foreground">{title.length}/200</span>
        </div>
        <Input
          id="support-title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Resumo do problema (ex: Impressora travada)"
          maxLength={200}
          required
          disabled={isSubmitting}
          className="text-xs"
        />
      </div>

      <div className="space-y-1">
        <div className="flex items-center justify-between">
          <label htmlFor="support-desc" className="text-xs font-semibold text-foreground block">
            Descrição do Problema <span className="text-destructive">*</span>
          </label>
          <span
            className={`text-[10px] ${
              remainingChars < 100 ? "text-amber-500 font-semibold" : "text-muted-foreground"
            }`}
          >
            {remainingChars} restantes
          </span>
        </div>
        <Textarea
          id="support-desc"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Descreva detalhadamente o ocorrido, mensagens de erro e orientações passadas ao usuário..."
          maxLength={8000}
          rows={4}
          required
          disabled={isSubmitting}
          className="text-xs resize-y"
        />
      </div>

      <div className="space-y-1">
        <label htmlFor="support-priority" className="text-xs font-semibold text-foreground block">
          Prioridade
        </label>
        <select
          id="support-priority"
          value={priority}
          onChange={(e) => setPriority(Number(e.target.value))}
          disabled={isSubmitting}
          className="w-full rounded-md border border-input bg-background px-3 py-1.5 text-xs shadow-xs focus:outline-hidden focus:ring-2 focus:ring-primary/40"
        >
          <option value={1}>1 - Muito Baixa</option>
          <option value={2}>2 - Baixa</option>
          <option value={3}>3 - Média (Padrão)</option>
          <option value={4}>4 - Alta</option>
          <option value={5}>5 - Muito Alta</option>
        </select>
      </div>

      <div className="border-t pt-2">
        <button
          type="button"
          onClick={() => setShowAdvanced((v) => !v)}
          className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
        >
          {showAdvanced ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
          Opções Avançadas GLPI
        </button>

        {showAdvanced && (
          <div className="grid grid-cols-2 gap-2.5 mt-2.5 pt-1">
            <div className="space-y-1">
              <label htmlFor="support-category" className="text-[11px] font-medium text-muted-foreground block">
                ID Categoria GLPI
              </label>
              <Input
                id="support-category"
                value={categoryId}
                onChange={(e) => setCategoryId(e.target.value.replace(/\D/g, ""))}
                placeholder="Ex: 4"
                maxLength={10}
                disabled={isSubmitting}
                className="text-xs font-mono"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="support-location" className="text-[11px] font-medium text-muted-foreground block">
                ID Localização GLPI
              </label>
              <Input
                id="support-location"
                value={locationId}
                onChange={(e) => setLocationId(e.target.value.replace(/\D/g, ""))}
                placeholder="Ex: 12"
                maxLength={10}
                disabled={isSubmitting}
                className="text-xs font-mono"
              />
            </div>
          </div>
        )}
      </div>

      {errorMessage && (
        <div className="rounded-md border border-destructive/30 bg-destructive/10 p-2.5 text-xs text-destructive flex items-start gap-2">
          <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
          <div className="flex-1 font-medium">{errorMessage}</div>
        </div>
      )}

      <div className="flex items-center justify-end gap-2 pt-2 border-t">
        {onCancel && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onCancel}
            disabled={isSubmitting}
          >
            Cancelar
          </Button>
        )}
        <Button
          type="submit"
          size="sm"
          disabled={isSubmitting || retryCountdown > 0 || !title.trim() || !description.trim()}
          className="min-w-[130px]"
        >
          {isSubmitting ? (
            <>
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              Criando Chamado...
            </>
          ) : retryCountdown > 0 ? (
            `Aguarde ${retryCountdown}s`
          ) : (
            <>
              <Ticket className="mr-1.5 h-3.5 w-3.5" />
              Criar Chamado GLPI
            </>
          )}
        </Button>
      </div>
    </form>
  );
};
