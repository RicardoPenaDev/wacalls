import { useEffect, useState } from "react";
import { Bell, Eye, EyeOff, Loader2, Save, Send } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  getEvolutionAlert,
  saveEvolutionAlert,
  testEvolutionAlert,
  type EvolutionAlertConfig,
} from "@/services/settings";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpdated?: (enabled: boolean) => void;
};

export const EvolutionAlertModal = ({ open, onOpenChange, onUpdated }: Props) => {
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [showApiKey, setShowApiKey] = useState(false);

  const [form, setForm] = useState<EvolutionAlertConfig>({
    enabled: false,
    apiUrl: "",
    apiKey: "",
    instanceName: "",
    destination: "",
  });

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    getEvolutionAlert()
      .then((cfg) => {
        if (cfg) {
          setForm({
            enabled: Boolean(cfg.enabled),
            apiUrl: cfg.apiUrl || "",
            apiKey: cfg.apiKey || "",
            instanceName: cfg.instanceName || "",
            destination: cfg.destination || "",
          });
        }
      })
      .catch((err) => {
        console.error("Falha ao carregar configurações da Evolution API:", err);
      })
      .finally(() => setLoading(false));
  }, [open]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await saveEvolutionAlert(form);
      toast.success("Configurações de alerta salvas com sucesso!");
      onUpdated?.(form.enabled);
      onOpenChange(false);
    } catch (err) {
      toast.error((err as Error).message || "Erro ao salvar configurações");
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    if (!form.apiUrl.trim() || !form.instanceName.trim() || !form.destination.trim()) {
      toast.error("Preencha a URL, a instância e o grupo/número de destino para testar.");
      return;
    }
    setTesting(true);
    try {
      const res = await testEvolutionAlert(form);
      toast.success(res.message || "Alerta de teste enviado com sucesso!");
    } catch (err) {
      toast.error((err as Error).message || "Falha ao enviar alerta de teste");
    } finally {
      setTesting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg gap-0 overflow-hidden p-0">
        {/* Header */}
        <div className="flex items-center justify-between border-b px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="grid h-9 w-9 place-items-center rounded-lg bg-primary/10 text-primary">
              <Bell className="h-4 w-4" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-semibold">Notificação de Queda / Conexão (Evolution API)</h3>
                {loading ? (
                  <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                ) : form.enabled ? (
                  <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-semibold text-emerald-500">
                    <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
                    Ativo
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground">
                    Inativo
                  </span>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                Envie alertas no WhatsApp quando qualquer instância conectar ou desconectar.
              </p>
            </div>
          </div>
        </div>

        {/* Body */}
        <div className="space-y-4 px-5 py-5">
          {/* Toggle checkbox */}
          <label className="flex cursor-pointer items-center gap-2.5 text-sm font-medium select-none">
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
              className="h-4 w-4 rounded border-border text-primary accent-primary focus:ring-primary"
            />
            <span>Enviar alerta de conexão/desconexão pra um grupo (via Evolution API)</span>
          </label>

          {/* Input fields */}
          <div className="space-y-3 pt-1">
            <div>
              <label className="mb-1 block text-xs font-medium text-foreground/80">
                URL da Evolution API
              </label>
              <Input
                value={form.apiUrl}
                onChange={(e) => setForm({ ...form, apiUrl: e.target.value })}
                placeholder="Ex: http://localhost:8080"
                className="h-9"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs font-medium text-foreground/80">
                Token (apikey)
              </label>
              <div className="relative">
                <Input
                  type={showApiKey ? "text" : "password"}
                  value={form.apiKey}
                  onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
                  placeholder="Sua apikey da instância"
                  className="h-9 pr-10"
                />
                <button
                  type="button"
                  onClick={() => setShowApiKey(!showApiKey)}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  aria-label={showApiKey ? "Ocultar apikey" : "Mostrar apikey"}
                >
                  {showApiKey ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs font-medium text-foreground/80">
                Nome da instância na Evolution API
              </label>
              <Input
                value={form.instanceName}
                onChange={(e) => setForm({ ...form, instanceName: e.target.value })}
                placeholder="Ex: teste1"
                className="h-9"
              />
            </div>

            <div>
              <label className="mb-1 block text-xs font-medium text-foreground/80">
                Grupo de destino (número/JID)
              </label>
              <Input
                value={form.destination}
                onChange={(e) => setForm({ ...form, destination: e.target.value })}
                placeholder="Ex: 120363012345678901@g.us"
                className="h-9"
              />
              <p className="mt-1 text-[11px] text-muted-foreground">
                Informe o JID do grupo (Ex: 120363...@g.us) ou o número de telefone com DDI (Ex: 5511999999999).
              </p>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t bg-muted/20 px-5 py-3.5">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleTest}
            disabled={testing || saving || !form.apiUrl || !form.instanceName || !form.destination}
            className="gap-1.5"
          >
            {testing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
            Testar Envio
          </Button>

          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => onOpenChange(false)}
            >
              Cancelar
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleSave}
              disabled={saving}
              className="gap-1.5"
            >
              {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
              Salvar
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
