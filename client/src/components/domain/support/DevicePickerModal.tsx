import { useEffect, useState } from "react";
import { Check, Laptop, Loader2, Monitor, Search, X } from "lucide-react";
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
import { Badge } from "@/components/ui/badge";
import { searchSupportDevices } from "@/services/support";
import type { DeviceBindingDTO } from "@/types/support";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentDeviceId?: string | null;
  onSelectDevice: (device: DeviceBindingDTO) => Promise<void> | void;
}

export const DevicePickerModal = ({
  open,
  onOpenChange,
  currentDeviceId,
  onSelectDevice,
}: Props) => {
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [devices, setDevices] = useState<DeviceBindingDTO[]>([]);
  const [selectedDevice, setSelectedDevice] = useState<DeviceBindingDTO | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setQuery("");
      setDevices([]);
      setSelectedDevice(null);
      setErrorMsg(null);
      return;
    }

    let active = true;
    const timer = setTimeout(async () => {
      setLoading(true);
      setErrorMsg(null);
      try {
        const res = await searchSupportDevices(query, 30);
        if (active) {
          setDevices(res.devices || []);
          // if currentDeviceId is set, pre-select it
          if (currentDeviceId && !selectedDevice) {
            const found = (res.devices || []).find((d) => d.id === currentDeviceId);
            if (found) setSelectedDevice(found);
          }
        }
      } catch (err: any) {
        if (active) {
          setErrorMsg(err?.message || "Erro ao consultar computadores");
        }
      } finally {
        if (active) setLoading(false);
      }
    }, 250);

    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [open, query, currentDeviceId]);

  const handleConfirm = async () => {
    if (!selectedDevice) return;
    setSubmitting(true);
    try {
      await onSelectDevice(selectedDevice);
      onOpenChange(false);
    } catch (e: any) {
      setErrorMsg(e?.message || "Falha ao vincular computador");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base font-semibold">
            <Monitor className="h-4 w-4 text-primary" />
            Vincular Computador ao Atendimento
          </DialogTitle>
          <DialogDescription className="text-xs text-muted-foreground">
            Localize o computador do mesmo cliente cadastrado no sistema para vincular ao chamado.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3 py-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Buscar por hostname, setor ou patrimônio..."
              className="pl-8 text-xs"
              autoFocus
            />
            {query && (
              <button
                type="button"
                onClick={() => setQuery("")}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          {errorMsg && (
            <p className="text-xs text-destructive font-medium">{errorMsg}</p>
          )}

          <div className="max-h-60 overflow-y-auto rounded-md border divide-y text-xs">
            {loading ? (
              <div className="flex items-center justify-center p-6 text-muted-foreground gap-2">
                <Loader2 className="h-4 w-4 animate-spin text-primary" />
                <span>Buscando computadores...</span>
              </div>
            ) : devices.length === 0 ? (
              <div className="p-6 text-center text-muted-foreground">
                <Laptop className="mx-auto h-6 w-6 opacity-40 mb-1" />
                <p>Nenhum computador encontrado.</p>
              </div>
            ) : (
              devices.map((device) => {
                const isSelected = selectedDevice?.id === device.id;
                const isCurrent = currentDeviceId === device.id;
                return (
                  <button
                    key={device.id}
                    type="button"
                    onClick={() => setSelectedDevice(device)}
                    className={`w-full flex items-center justify-between p-2.5 text-left transition hover:bg-muted/50 ${
                      isSelected ? "bg-primary/10 hover:bg-primary/15" : ""
                    }`}
                  >
                    <div className="min-w-0 pr-2">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <span className="font-semibold text-foreground truncate">
                          {device.hostname}
                        </span>
                        {isCurrent && (
                          <Badge variant="outline" className="text-[9px] px-1 py-0 bg-muted">
                            Atual
                          </Badge>
                        )}
                        {device.matchStatus === "matched" && (
                          <Badge variant="outline" className="text-[9px] px-1 py-0 bg-emerald-500/10 text-emerald-600 border-emerald-500/20">
                            GLPI OK
                          </Badge>
                        )}
                      </div>
                      <div className="flex items-center gap-2 mt-0.5 text-muted-foreground text-[11px]">
                        {device.sectorCode && <span>Setor: {device.sectorCode}</span>}
                        {device.patrimonio && <span>Patrimônio: {device.patrimonio}</span>}
                      </div>
                    </div>
                    {isSelected && (
                      <Check className="h-4 w-4 shrink-0 text-primary" />
                    )}
                  </button>
                );
              })
            )}
          </div>
        </div>

        <DialogFooter className="gap-2 sm:gap-0">
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
            type="button"
            size="sm"
            onClick={handleConfirm}
            disabled={!selectedDevice || submitting}
          >
            {submitting ? (
              <>
                <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                Vinculando...
              </>
            ) : (
              "Confirmar Vinculação"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
