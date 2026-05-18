package cli

import (
	"context"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/i18n"
)

const installLangEnv = "HOPCLAW_INSTALL_LANG"

type installLang string

const (
	installLangEnglish installLang = "en"
	installLangChinese installLang = "zh"
)

func normalizeInstallLang(raw string) installLang {
	normalized := strings.TrimSpace(strings.ToLower(raw))
	switch {
	case normalized == "":
		return ""
	case strings.HasPrefix(normalized, "zh"), normalized == "cn", normalized == "chinese":
		return installLangChinese
	case strings.HasPrefix(normalized, "en"), normalized == "english":
		return installLangEnglish
	default:
		return ""
	}
}

func currentInstallLang() installLang {
	if lang := normalizeInstallLang(os.Getenv(installLangEnv)); lang != "" {
		return lang
	}
	if currentInstallLocale() == i18n.ZhCN {
		return installLangChinese
	}
	return installLangEnglish
}

func currentInstallLocale() i18n.Locale {
	if lang := normalizeInstallLang(os.Getenv(installLangEnv)); lang != "" {
		if lang == installLangChinese {
			return i18n.ZhCN
		}
		return i18n.EN
	}
	return i18n.DetectLocale()
}

func installLangIsChinese() bool {
	return currentInstallLang() == installLangChinese
}

func itext(english, chinese string) string {
	if key := installCatalogKey(english); key != "" {
		return itextKey(key, english, chinese)
	}
	return fallbackInstallText(english, chinese)
}

func installCatalogKey(english string) string {
	trimmed := strings.TrimSpace(english)
	if trimmed == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(trimmed))
	lastUnderscore := false
	for _, r := range trimmed {
		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteByte(byte(r + ('a' - 'A')))
			lastUnderscore = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	slug := strings.Trim(builder.String(), "_")
	if slug == "" {
		return ""
	}
	return "cli.install_text." + slug
}

func itextKey(key, english, chinese string, params ...string) string {
	if strings.TrimSpace(key) != "" {
		locale := currentInstallLocale()
		msg := i18n.TCtx(i18n.WithLocale(context.Background(), locale), key, params...)
		if strings.TrimSpace(msg) != "" && msg != key {
			return msg
		}
	}
	return fallbackInstallText(english, chinese)
}

func fallbackInstallText(english, chinese string) string {
	if installLangIsChinese() && strings.TrimSpace(chinese) != "" {
		return chinese
	}
	return english
}
