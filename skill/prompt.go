package skill

import (
	"fmt"
	"strings"
)

func FormatPromptCatalog(entries []PromptCatalogEntry) string {
	return FormatPromptCatalogWithNotice(entries, 0, "")
}

func FormatPromptCatalogWithNotice(entries []PromptCatalogEntry, omitted int, notice string) string {
	var b strings.Builder
	b.WriteString("<skills>\n")
	for _, entry := range entries {
		b.WriteString(`  <skill name="`)
		b.WriteString(xmlEscape(entry.Name))
		b.WriteString(`" location="`)
		b.WriteString(xmlEscape(entry.Location))
		b.WriteString(`">`)
		if entry.Description != "" {
			b.WriteString(xmlEscape(entry.Description))
		}
		b.WriteString("</skill>\n")
	}
	if omitted > 0 {
		b.WriteString("  <note>")
		if strings.TrimSpace(notice) != "" {
			b.WriteString(xmlEscape(strings.TrimSpace(notice)))
			b.WriteString(" ")
		}
		b.WriteString(xmlEscape(fmt.Sprintf("%d additional skills omitted.", omitted)))
		b.WriteString("</note>\n")
	}
	b.WriteString("</skills>")
	return b.String()
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}
