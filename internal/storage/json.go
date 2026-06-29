package storage

import (
	"encoding/json"
	"os"
)

// LoadSettings charge les paramètres depuis settings.json ou utilise les valeurs par défaut
func LoadSettings(cfg Config) Settings {
	var s Settings

	s.Subject = cfg.SenderName + " - Candidature"
	s.PortfolioURL = "https://portfolio.example.com"
	s.Links = []Link{}

	file, err := os.Open("settings.json")
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&s)
	}

	// Si la liste des liens est vide et qu'il y a un portfolio URL, on initialise le lien par défaut
	if len(s.Links) == 0 && s.PortfolioURL != "" {
		s.Links = append(s.Links, Link{
			Key:   "Portfolio",
			Label: "Portfolio",
			URL:   s.PortfolioURL,
		})
	}
	return s
}

// SaveSettings enregistre les paramètres dans settings.json
func SaveSettings(s Settings) error {
	file, err := os.Create("settings.json")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(s)
}

// LoadTemplates charge les modèles depuis templates.json ou importe les fichiers existants
func LoadTemplates() []EmailTemplate {
	var templates []EmailTemplate
	file, err := os.Open("templates.json")
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&templates)
	}

	// Si aucun modèle n'est enregistré, on tente d'importer les fichiers existants
	if len(templates) == 0 {
		// Modèle HTML
		htmlBodyBytes, errHtml := os.ReadFile("assets/templates/template.html")
		if errHtml == nil {
			templates = append(templates, EmailTemplate{
				ID:      "default_html",
				Name:    "Modèle Alternance (HTML)",
				Subject: "Candidature Alternance",
				Body:    string(htmlBodyBytes),
				Type:    "html",
			})
		}
		// Modèle Texte
		txtBodyBytes, errTxt := os.ReadFile("assets/templates/template.txt")
		if errTxt == nil {
			templates = append(templates, EmailTemplate{
				ID:      "default_txt",
				Name:    "Modèle Alternance (Texte)",
				Subject: "Candidature Alternance",
				Body:    string(txtBodyBytes),
				Type:    "txt",
			})
		}

		// Si toujours vide
		if len(templates) == 0 {
			templates = append(templates, EmailTemplate{
				ID:      "default",
				Name:    "Modèle par défaut",
				Subject: "Candidature Alternance",
				Body:    "Bonjour {{.FirstName}} {{.LastName}},\n\nJe vous adresse ma candidature...",
				Type:    "txt",
			})
		}
		SaveTemplates(templates)
	}
	return templates
}

// SaveTemplates enregistre les modèles dans templates.json
func SaveTemplates(templates []EmailTemplate) error {
	file, err := os.Create("templates.json")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(templates)
}
