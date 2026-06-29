package web

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net/smtp"
	"time"

	"github.com/ozneyy/mailternance/internal/config"
	"github.com/ozneyy/mailternance/internal/email"
	"github.com/ozneyy/mailternance/internal/storage"
)

// RunEmailSender gère la campagne d'envoi d'e-mails (CLI / Cron)
func RunEmailSender() {
	cfg := config.GetActiveConfig()
	log.Println("[INFO] Mode Envoi CLI : Démarrage de la campagne d'e-mails...")

	// Charger les paramètres
	settings := storage.LoadSettings(cfg)

	// Charger les pièces jointes
	attachments, err := getAttachmentsListBytes()
	if err != nil {
		log.Fatalf("[FATAL] Impossible de lire les pièces jointes : %v", err)
	}

	// Charger les modèles et utiliser le premier modèle par défaut
	templatesList := storage.LoadTemplates()
	var selectedTemplate *storage.EmailTemplate
	if len(templatesList) > 0 {
		selectedTemplate = &templatesList[0]
	}

	if selectedTemplate == nil {
		log.Fatalf("[FATAL] Aucun modèle d'e-mail disponible.")
	}

	isHTML := selectedTemplate.Type == "html"
	bodyText := email.PreprocessTemplateBody(selectedTemplate.Body, settings.Links)
	tmpl, err := template.New("email").Parse(bodyText)
	if err != nil {
		log.Fatalf("[FATAL] Impossible de parser le template %s : %v", selectedTemplate.Name, err)
	}

	// Charger les destinataires
	recipients, err := email.LoadRecipients(cfg.CSVPath)
	if err != nil {
		log.Fatalf("[FATAL] Impossible de charger le fichier CSV (%s) : %v", cfg.CSVPath, err)
	}

	if len(recipients) == 0 {
		log.Println("[INFO] Aucun destinataire à traiter. Fin du script.")
		return
	}

	log.Printf("[INFO] Fichier CSV chargé. Envoi à %d destinataires...", len(recipients))
	successCount := 0
	failureCount := 0
	auth := smtp.PlainAuth("", cfg.SMTPEmail, cfg.SMTPPassword, cfg.SMTPHost)

	for i, recipient := range recipients {
		emailAddr, exists := recipient["Email"]
		if !exists || emailAddr == "" {
			log.Printf("[WARNING] Ligne %d sautée : adresse email manquante", i+2)
			failureCount++
			continue
		}

		if !email.IsValidEmail(emailAddr) {
			log.Printf("[WARNING] Ligne %d sautée : format email invalide (%s)", i+2, emailAddr)
			failureCount++
			continue
		}

		log.Printf("[INFO] [%d/%d] Envoi du mail à : %s", i+1, len(recipients), emailAddr)

		dataMap := make(map[string]interface{})
		for k, v := range recipient {
			dataMap[k] = v
		}
		dataMap["PortfolioURL"] = settings.PortfolioURL
		dataMap["SenderName"] = cfg.SenderName

		// Construire la map des liens
		linksMap := make(map[string]interface{})
		for _, l := range settings.Links {
			if isHTML {
				linksMap[l.Key] = template.HTML(fmt.Sprintf(`<a href="%s" style="color: #3b82f6; text-decoration: underline; font-weight: 600;">%s</a>`, l.URL, l.Label))
			} else {
				linksMap[l.Key] = fmt.Sprintf("%s (%s)", l.Label, l.URL)
			}
		}
		dataMap["Links"] = linksMap

		var bodyBuffer bytes.Buffer
		err := tmpl.Execute(&bodyBuffer, dataMap)
		if err != nil {
			log.Printf("[ERROR] Erreur template pour %s : %v", emailAddr, err)
			failureCount++
			continue
		}

		msg := email.BuildMultipartMessage(cfg.SenderName, cfg.SMTPEmail, emailAddr, selectedTemplate.Subject, bodyBuffer.String(), attachments)
		addr := fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort)

		err = smtp.SendMail(addr, auth, cfg.SMTPEmail, []string{emailAddr}, msg)
		if err != nil {
			log.Printf("[ERROR] Échec de l'envoi à %s : %v", emailAddr, err)
			failureCount++
		} else {
			log.Printf("[SUCCESS] E-mail envoyé à %s", emailAddr)
			successCount++
			storage.SaveSentRecord(storage.SentRecord{
				Email:      emailAddr,
				Subject:    selectedTemplate.Subject,
				Date:       time.Now(),
				TemplateID: selectedTemplate.ID,
			})
		}

		if i < len(recipients)-1 && cfg.SendDelayMs > 0 {
			time.Sleep(time.Duration(cfg.SendDelayMs) * time.Millisecond)
		}
	}

	log.Printf("[INFO] Campagne terminée. Réussites : %d, Échecs : %d", successCount, failureCount)
}
