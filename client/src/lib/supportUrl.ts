/**
 * Valida defensivamente a webUrl fornecida pelo backend para navegação segura no GLPI.
 * Aceita somente URLs que atendam simultaneamente a todos os critérios:
 * protocolo https:, sem userinfo, pathname exato /front/ticket.form.php,
 * sem hash/fragmento, exatamente um único par de query string cuja chave é
 * "id" (sem duplicação, sem parâmetros adicionais mesmo vazios) e cujo valor
 * é um decimal positivo canônico (/^[1-9]\d*$/). Qualquer desvio retorna
 * null; nunca lança exceção.
 */
export function sanitizeGLPIWebUrl(rawUrl?: string | null): string | null {
  if (!rawUrl || typeof rawUrl !== "string") {
    return null;
  }
  const trimmed = rawUrl.trim();
  if (!trimmed) {
    return null;
  }
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== "https:") {
      return null;
    }
    if (parsed.username || parsed.password) {
      return null;
    }
    if (parsed.pathname !== "/front/ticket.form.php") {
      return null;
    }
    if (parsed.hash !== "") {
      return null;
    }
    const pairs = Array.from(parsed.searchParams.entries());
    if (pairs.length !== 1) {
      return null;
    }
    const [key, value] = pairs[0];
    if (key !== "id") {
      return null;
    }
    if (!/^[1-9]\d*$/.test(value)) {
      return null;
    }
    return parsed.href;
  } catch {
    return null;
  }
}
