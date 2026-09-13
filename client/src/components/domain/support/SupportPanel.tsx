import { useCallback, useEffect, useRef, useState } from "react";
import {
  AlertTriangle,
  Headset,
  Loader2,
  Plus,
  RefreshCw,
  Ticket,
  Wrench,
} from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { DeviceSummary } from "./DeviceSummary";
import { TacticalStatus } from "./TacticalStatus";
import { SupportTicketForm } from "./SupportTicketForm";
import { SupportRequestStatus } from "./SupportRequestStatus";
import { DevicePickerModal } from "./DevicePickerModal";
import { useAuth } from "@/stores/auth";
import { useOptionsStore } from "@/stores/options";
import {
  getChatSupportContext,
  getSupportDevice,
  getSupportRequest,
  updateSupportRequestDevice,
  SupportApiError,
} from "@/services/support";
import type { ChatSummary } from "@/types/chat";
import type {
  DeviceBindingDTO,
  SupportRequestDTO,
  SupportTicketResponseEnvelope,
  TacticalAgentSummary,
} from "@/types/support";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  sessionId: string;
  chatJid: string;
  chat?: ChatSummary;
}

export const SupportPanel = ({
  open,
  onOpenChange,
  sessionId,
  chatJid,
  chat,
}: Props) => {
  const currentUser = useAuth((s) => s.user);
  const options = useOptionsStore((s) => s.options);
  const tacticalEnabled = !!options.features?.tactical;

  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  // Active state
  const [currentRequest, setCurrentRequest] = useState<SupportRequestDTO | null>(null);
  const [device, setDevice] = useState<DeviceBindingDTO | null>(null);
  const [tacticalAgent, setTacticalAgent] = useState<TacticalAgentSummary | null>(null);
  const [warnings, setWarnings] = useState<string[]>([]);
  const [isCreatingNew, setIsCreatingNew] = useState(false);

  // Device picker modal state
  const [showDevicePicker, setShowDevicePicker] = useState(false);

  // Polling tracking for processing state
  const pollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pollCountRef = useRef<number>(0);

  const clearPoll = () => {
    if (pollTimerRef.current) {
      clearTimeout(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    pollCountRef.current = 0;
  };

  const loadContext = useCallback(
    async (isManualRefresh = false) => {
      if (!sessionId || !chatJid) return;
      if (isManualRefresh) setRefreshing(true);
      else setLoading(true);
      setErrorMsg(null);
      clearPoll();

      try {
        const ctx = await getChatSupportContext(sessionId, chatJid);
        const req = ctx.currentSupportRequest || null;
        setCurrentRequest(req);
        setIsCreatingNew(false);

        // If there's an associated device binding ID on the ticket, load full device details
        const devId = req?.deviceBindingId;
        if (devId) {
          try {
            const devDetail = await getSupportDevice(devId);
            setDevice(devDetail.device);
            setTacticalAgent(devDetail.tacticalAgent);
            setWarnings(devDetail.warnings || []);
          } catch {
            // non-fatal device load failure
          }
        } else {
          setDevice(null);
          setTacticalAgent(null);
          setWarnings([]);
        }

        // If ticket is in processing state, trigger poll
        if (req?.syncState === "processing") {
          startProcessingPoll(req.id);
        }
      } catch (err: any) {
        if (err instanceof SupportApiError && err.status === 503) {
          setErrorMsg("Módulo de suporte GLPI desabilitado no servidor.");
        } else {
          setErrorMsg(err?.message || "Erro ao carregar dados de suporte.");
        }
      } finally {
        setLoading(false);
        setRefreshing(false);
      }
    },
    [sessionId, chatJid],
  );

  const startProcessingPoll = (reqId: string) => {
    clearPoll();
    const poll = async () => {
      pollCountRef.current += 1;
      try {
        const env = await getSupportRequest(reqId);
        setCurrentRequest(env.supportRequest);
        if (env.device) setDevice(env.device);
        if (env.tacticalAgent) setTacticalAgent(env.tacticalAgent);
        if (env.warnings) setWarnings(env.warnings);

        // Cease polling if state resolved from processing
        if (env.supportRequest.syncState !== "processing") {
          clearPoll();
          return;
        }
      } catch {
        // if request failed, keep poll going until max count
      }

      if (pollCountRef.current < 5) {
        pollTimerRef.current = setTimeout(poll, 3000);
      } else {
        clearPoll();
      }
    };

    pollTimerRef.current = setTimeout(poll, 3000);
  };

  useEffect(() => {
    if (open) {
      void loadContext();
    } else {
      clearPoll();
    }
    return () => {
      clearPoll();
    };
  }, [open, loadContext]);

  const handleEnvelopeUpdate = (env: SupportTicketResponseEnvelope) => {
    setCurrentRequest(env.supportRequest);
    if (env.device !== undefined) setDevice(env.device);
    if (env.tacticalAgent !== undefined) setTacticalAgent(env.tacticalAgent);
    if (env.warnings !== undefined) setWarnings(env.warnings);
    setIsCreatingNew(false);

    if (env.supportRequest.syncState === "processing") {
      startProcessingPoll(env.supportRequest.id);
    } else {
      clearPoll();
    }
  };

  const handleSelectDevice = async (selected: DeviceBindingDTO) => {
    if (currentRequest) {
      // Update existing support request device
      const updated = await updateSupportRequestDevice(currentRequest.id, {
        deviceBindingId: selected.id,
      });
      handleEnvelopeUpdate(updated);
    } else {
      // Update in-form selected device
      setDevice(selected);
      // Fetch tactical agent for the newly selected device if tactical is enabled
      if (tacticalEnabled && selected.id) {
        try {
          const detail = await getSupportDevice(selected.id);
          setTacticalAgent(detail.tacticalAgent);
          setWarnings(detail.warnings || []);
        } catch {
          // ignore
        }
      }
    }
  };

  const defaultRequester = chat?.name || chatJid.split("@")[0] || "";

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-full p-0 sm:max-w-md">
        <ScrollArea className="h-full">
          <SheetHeader className="border-b bg-gradient-to-b from-primary/10 via-primary/5 to-transparent px-5 py-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <div className="grid h-8 w-8 place-items-center rounded-lg bg-primary/15 text-primary">
                  <Headset className="h-4 w-4" />
                </div>
                <div>
                  <SheetTitle className="text-sm font-semibold">
                    Suporte GLPI & Telemetria
                  </SheetTitle>
                  <SheetDescription className="text-[11px] text-muted-foreground">
                    Atendimento integrado ao GLPI e inventário
                  </SheetDescription>
                </div>
              </div>

              <Button
                size="sm"
                variant="ghost"
                onClick={() => void loadContext(true)}
                disabled={loading || refreshing}
                title="Recarregar dados de suporte"
                className="h-8 w-8 p-0"
              >
                <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`} />
              </Button>
            </div>
          </SheetHeader>

          <div className="p-5 space-y-5">
            {loading ? (
              <div className="flex flex-col items-center justify-center py-16 text-muted-foreground gap-2">
                <Loader2 className="h-6 w-6 animate-spin text-primary" />
                <span className="text-xs">Carregando painel de suporte...</span>
              </div>
            ) : errorMsg ? (
              <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-3.5 text-xs text-destructive space-y-2">
                <div className="flex items-center gap-2 font-semibold">
                  <AlertTriangle className="h-4 w-4" />
                  <span>Atenção</span>
                </div>
                <p>{errorMsg}</p>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => void loadContext()}
                  className="mt-2 text-xs"
                >
                  Tentar Novamente
                </Button>
              </div>
            ) : (
              <>
                {/* Real-time Tactical telemetry (rendered only when features.tactical = true) */}
                <TacticalStatus
                  tacticalEnabled={tacticalEnabled}
                  tacticalAgent={tacticalAgent}
                  warnings={warnings}
                />

                {/* Main section: Active request status or Creation form */}
                {currentRequest && !isCreatingNew ? (
                  <div className="space-y-4">
                    <SupportRequestStatus
                      request={currentRequest}
                      device={device}
                      currentUser={currentUser}
                      onUpdate={handleEnvelopeUpdate}
                      onNewTicketClick={() => setIsCreatingNew(true)}
                      onChangeDeviceClick={() => setShowDevicePicker(true)}
                    />

                    {currentRequest.syncState === "synced" && (
                      <div className="pt-2 border-t flex justify-end">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setIsCreatingNew(true)}
                          className="text-xs text-muted-foreground hover:text-foreground"
                        >
                          <Plus className="mr-1.5 h-3.5 w-3.5" />
                          Abrir Outro Chamado
                        </Button>
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="space-y-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1.5 font-semibold text-xs text-foreground">
                        <Ticket className="h-3.5 w-3.5 text-primary" />
                        <span>Novo Chamado GLPI</span>
                      </div>
                      {currentRequest && (
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setIsCreatingNew(false)}
                          className="text-xs h-7 px-2"
                        >
                          Voltar ao Chamado Ativo
                        </Button>
                      )}
                    </div>

                    <SupportTicketForm
                      sessionId={sessionId}
                      chatJid={chatJid}
                      defaultRequesterName={defaultRequester}
                      selectedDevice={device}
                      onChangeDeviceClick={() => setShowDevicePicker(true)}
                      onSuccess={handleEnvelopeUpdate}
                      onCancel={currentRequest ? () => setIsCreatingNew(false) : undefined}
                    />
                  </div>
                )}
              </>
            )}
          </div>
        </ScrollArea>

        {/* Modal for picking / switching equipment */}
        <DevicePickerModal
          open={showDevicePicker}
          onOpenChange={setShowDevicePicker}
          currentDeviceId={device?.id}
          onSelectDevice={handleSelectDevice}
        />
      </SheetContent>
    </Sheet>
  );
};
