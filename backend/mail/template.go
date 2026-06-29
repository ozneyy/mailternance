package mail

import (
	"fmt"
	"strings"

	"github.com/ozneyy/mailternance/backend/templates"
)

// PreprocessTemplateBody convertit proprement les accolades simples en double accolades
// sans altérer les accolades doubles déjà existantes.
func PreprocessTemplateBody(body string, links []templates.Link) string {
	// 1. Sauvegarder les doubles accolades valides dans des jetons uniques
	body = strings.ReplaceAll(body, "{{.FirstName}}", "##FN##")
	body = strings.ReplaceAll(body, "{{.LastName}}", "##LN##")
	body = strings.ReplaceAll(body, "{{.Company}}", "##CO##")
	body = strings.ReplaceAll(body, "{{.Position}}", "##PO##")
	body = strings.ReplaceAll(body, "{{.PortfolioURL}}", "##PURL##")
	body = strings.ReplaceAll(body, "{{.SenderName}}", "##SN##")
	for _, l := range links {
		body = strings.ReplaceAll(body, fmt.Sprintf("{{.Links.%s}}", l.Key), fmt.Sprintf("##LK_%s##", l.Key))
	}

	// 2. Remplacer les accolades simples par les doubles accolades
	body = strings.ReplaceAll(body, "{.FirstName}", "{{.FirstName}}")
	body = strings.ReplaceAll(body, "{.LastName}", "{{.LastName}}")
	body = strings.ReplaceAll(body, "{.Company}", "{{.Company}}")
	body = strings.ReplaceAll(body, "{.Position}", "{{.Position}}")
	body = strings.ReplaceAll(body, "{.PortfolioURL}", "{{.PortfolioURL}}")
	body = strings.ReplaceAll(body, "{.SenderName}", "{{.SenderName}}")
	for _, l := range links {
		body = strings.ReplaceAll(body, fmt.Sprintf("{.Links.%s}", l.Key), fmt.Sprintf("{{.Links.%s}}", l.Key))
	}

	// 3. Restaurer les jetons uniques en doubles accolades correctes
	body = strings.ReplaceAll(body, "##FN##", "{{.FirstName}}")
	body = strings.ReplaceAll(body, "##LN##", "{{.LastName}}")
	body = strings.ReplaceAll(body, "##CO##", "{{.Company}}")
	body = strings.ReplaceAll(body, "##PO##", "{{.Position}}")
	body = strings.ReplaceAll(body, "##PURL##", "{{.PortfolioURL}}")
	body = strings.ReplaceAll(body, "##SN##", "{{.SenderName}}")
	for _, l := range links {
		body = strings.ReplaceAll(body, fmt.Sprintf("##LK_%s##", l.Key), fmt.Sprintf("{{.Links.%s}}", l.Key))
	}

	return body
}
