import { Check, Globe } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SUPPORTED_LANGS, changeLanguage, type LangCode } from "@/i18n";

const FlagIcon = ({ flag, country, className }: { flag?: string; country?: string; className?: string }) => (
  <span className={className ?? "text-base leading-none select-none"} aria-hidden>
    {flag || (country ? country.toUpperCase() : "🌐")}
  </span>
);

/**
 * Compact flag + ISO code language picker for the header.
 * Persists choice to localStorage and best-effort syncs to user profile.
 */
export const LanguageSwitcher = () => {
  const { t, i18n } = useTranslation();
  const current = SUPPORTED_LANGS.find((l) => i18n.language?.startsWith(l.code.split("-")[0])) ?? SUPPORTED_LANGS[0];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={t("language.change")}
          className="flex h-9 items-center gap-1.5 rounded-full border border-border/60 bg-muted/40 px-2.5 text-xs font-semibold text-foreground transition hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <FlagIcon flag={current.flag} country={current.country} />
          <span className="tracking-wide">{current.short}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel className="flex items-center gap-2 text-xs">
          <Globe className="h-3.5 w-3.5" /> {t("language.label")}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {SUPPORTED_LANGS.map((lng) => {
          const active = current.code === lng.code;
          return (
            <DropdownMenuItem
              key={lng.code}
              onSelect={() => void changeLanguage(lng.code as LangCode)}
              className="gap-2"
            >
              <FlagIcon flag={lng.flag} country={lng.country} />
              <span className="flex-1">{lng.label}</span>
              {active && <Check className="h-4 w-4 text-nav-icon" />}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};