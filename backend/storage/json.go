package storage

import (
	"encoding/json"
	"os"

	"github.com/ozneyy/mailternance/backend/templates"
)

// LoadSettings charge les paramètres depuis settings.json ou utilise les valeurs par défaut
func LoadSettings(cfg templates.Config) templates.Settings {
	var s templates.Settings

	s.Subject = cfg.SenderName + " - Candidature"
	s.PortfolioURL = "https://portfolio.example.com"
	s.Links = []templates.Link{}

	file, err := os.Open(GetStoragePath("settings.json"))
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&s)
	}

	// Si la liste des liens est vide et qu'il y a un portfolio URL, on initialise le lien par défaut
	if len(s.Links) == 0 && s.PortfolioURL != "" {
		s.Links = append(s.Links, templates.Link{
			Key:   "Portfolio",
			Label: "Portfolio",
			URL:   s.PortfolioURL,
		})
	}
	return s
}

// SaveSettings enregistre les paramètres dans settings.json
func SaveSettings(s templates.Settings) error {
	file, err := os.Create(GetStoragePath("settings.json"))
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(s)
}

// LoadTemplates charge les modèles depuis templates.json ou crée les modèles par défaut
func LoadTemplates() []templates.EmailTemplate {
	var list []templates.EmailTemplate
	file, err := os.Open(GetStoragePath("templates.json"))
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&list)
	}

	// Si aucun modèle n'est enregistré, on tente d'importer les fichiers existants depuis le dossier web/
	if len(list) == 0 {
		// Modèle HTML
		htmlBodyBytes, errHtml := os.ReadFile("web/templates/template.html")
		if errHtml == nil {
			list = append(list, templates.EmailTemplate{
				ID:      "default_html",
				Name:    "Modèle Alternance (HTML)",
				Subject: "Candidature Alternance",
				Body:    string(htmlBodyBytes),
				Type:    "html",
			})
		}
		// Modèle Texte
		txtBodyBytes, errTxt := os.ReadFile("web/templates/template.txt")
		if errTxt == nil {
			list = append(list, templates.EmailTemplate{
				ID:      "default_txt",
				Name:    "Modèle Alternance (Texte)",
				Subject: "Candidature Alternance",
				Body:    string(txtBodyBytes),
				Type:    "txt",
			})
		}

		// Si toujours vide, créer un modèle par défaut
		if len(list) == 0 {
			list = append(list, templates.EmailTemplate{
				ID:      "default",
				Name:    "Modèle par défaut",
				Subject: "Candidature Alternance",
				Body:    "Bonjour {{.FirstName}} {{.LastName}},\n\nJe vous adresse ma candidature pour le poste de {{.Position}} chez {{.Company}}.\n\nCordialement,\n{{.SenderName}}",
				Type:    "txt",
			})
		}
		SaveTemplates(list)
	}
	return list
}

// SaveTemplates enregistre les modèles dans templates.json
func SaveTemplates(list []templates.EmailTemplate) error {
	file, err := os.Create(GetStoragePath("templates.json"))
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(list)
}

// GetAttachmentsList lit le dossier web/attachments/ et retourne la liste des noms de fichiers
func GetAttachmentsList() ([]string, error) {
	err := os.MkdirAll("web/attachments", 0755)
	if err != nil {
		return nil, err
	}

	files, err := os.ReadDir("web/attachments")
	if err != nil {
		return nil, err
	}

	var list []string
	for _, f := range files {
		if !f.IsDir() {
			list = append(list, f.Name())
		}
	}
	return list, nil
}

// GetAttachmentsListBytes lit les fichiers du dossier web/attachments/ et les renvoie sous forme d'octets
func GetAttachmentsListBytes() ([]templates.Attachment, error) {
	dirEntries, err := os.ReadDir("web/attachments")
	if err != nil {
		return nil, err
	}

	var list []templates.Attachment
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		path := "web/attachments/" + entry.Name()
		b, errRead := os.ReadFile(path)
		if errRead != nil {
			continue
		}
		list = append(list, templates.Attachment{
			Name:  entry.Name(),
			Bytes: b,
		})
	}
	return list, nil
}

// LoadAutoSync charge la configuration de synchronisation automatique
func LoadAutoSync() templates.AutoSyncConfig {
	var c templates.AutoSyncConfig
	c.Enabled = false
	c.Interval = 300 // 5 minutes par défaut (300 secondes)

	file, err := os.Open(GetStoragePath("autosync.json"))
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&c)
	}
	return c
}

// SaveAutoSync enregistre la configuration de synchronisation automatique
func SaveAutoSync(c templates.AutoSyncConfig) error {
	file, err := os.Create(GetStoragePath("autosync.json"))
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(c)
}

// LoadAutoSend charge la configuration d'envoi automatique
func LoadAutoSend() templates.AutoSendConfig {
	var c templates.AutoSendConfig
	c.Enabled = false
	c.Interval = 3600 // 60 minutes par défaut (3600 secondes)
	c.TemplateID = ""
	c.SkipAlreadySent = true

	file, err := os.Open(GetStoragePath("autosend.json"))
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&c)
	}
	return c
}

// SaveAutoSend enregistre la configuration d'envoi automatique
func SaveAutoSend(c templates.AutoSendConfig) error {
	file, err := os.Create(GetStoragePath("autosend.json"))
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(c)
}

