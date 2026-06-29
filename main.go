package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
)

// Config contient les valeurs de configuration de l'application chargées de l'environnement
type Config struct {
	SMTPHost     string
	SMTPPort     string
	IMAPHost     string
	IMAPPort     string
	Port         string
	SMTPEmail    string
	SMTPPassword string
	SenderName   string
	CSVPath      string
	TemplatePath string
	SendDelayMs  int
}

// Link représente un lien personnalisé avec une clef, un libellé et une URL
type Link struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Settings représente les paramètres modifiables via l'interface web (sujet, portfolio, liens)
type Settings struct {
	Subject      string `json:"subject"`
	PortfolioURL string `json:"portfolioUrl"`
	Links        []Link `json:"links"`
}

// EmailTemplate représente un modèle d'e-mail nommé
type EmailTemplate struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Type    string `json:"type"` // "html" ou "txt"
}

// Reply représente une réponse d'e-mail stockée localement
type Reply struct {
	Email   string    `json:"email"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Body    string    `json:"body"`
	Snippet string    `json:"snippet"`
}

// ReplyEvent représente une réponse simplifiée pour le dashboard
type ReplyEvent struct {
	Subject string    `json:"Subject"`
	Date    time.Time `json:"Date"`
	Snippet string    `json:"Snippet"`
	Body    string    `json:"Body"`
}

// SentRecord représente un enregistrement d'envoi d'e-mail persisté dans sent_history.json
type SentRecord struct {
	Email      string    `json:"email"`
	Subject    string    `json:"subject"`
	Date       time.Time `json:"date"`
	TemplateID string    `json:"templateId"`
}

// SentEvent représente un événement d'envoi d'e-mail pour le dashboard
type SentEvent struct {
	Subject string    `json:"Subject"`
	Date    time.Time `json:"Date"`
}

// DashboardItem combine les données CSV et la dernière réponse reçue
type DashboardItem struct {
	Email        string
	FirstName    string
	LastName     string
	Company      string
	Position     string
	HasReply     bool
	ReplyCount   int         `json:"ReplyCount"`
	ReplySubject string
	ReplySnippet string
	ReplyDate    string
	SentCount    int         `json:"SentCount"`
	SentHistory  []SentEvent `json:"SentHistory"`
	ReplyHistory []ReplyEvent `json:"ReplyHistory"`
}

// Attachment stocke les informations sur une pièce jointe
type Attachment struct {
	Name  string
	Bytes []byte
}

// EnvData représente la structure du fichier .env pour l'interface web
type EnvData struct {
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     string `json:"smtpPort"`
	IMAPHost     string `json:"imapHost"`
	IMAPPort     string `json:"imapPort"`
	Port         string `json:"port"`
	SMTPEmail    string `json:"smtpEmail"`
	SMTPPassword string `json:"smtpPassword"`
	SenderName   string `json:"senderName"`
	SendDelayMs  string `json:"sendDelayMs"`
}

// DashboardPageData est la structure passée au template dashboard.html
type DashboardPageData struct {
	TotalSent       int
	TotalReplies    int
	TotalWaiting    int
	ReplyRate       int
	ItemsJSON       template.JS
	RepliesJSON     template.JS
	SettingsJSON    template.JS
	TemplateJSON    template.JS
	TemplatesJSON   template.JS
	AttachmentsJSON template.JS
	EnvJSON         template.JS
}

// Variables globales partagées pour la configuration thread-safe et l'état de la campagne d'envoi
var (
	configMutex  sync.RWMutex
	activeConfig Config

	sendMutex        sync.Mutex
	sendInProgress   bool
	sendTotal        int
	sendCurrent      int
	sendLastLog      string
	sendSuccessCount int
	sendFailureCount int
)

func main() {
	// 1. Définir et analyser les arguments de la ligne de commande
	webMode := flag.Bool("web", false, "Démarrer le serveur web de suivi (dashboard)")
	flag.Parse()

	// 2. Charger le fichier .env
	loadEnv(".env")

	// 3. Charger la configuration globale initiale
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Erreur de configuration initiale : %v", err)
	}
	setActiveConfig(config)

	// 4. S'assurer que le dossier des pièces jointes existe
	os.MkdirAll("attachments", 0755)

	log.Printf("[INFO] Configuration chargée — SMTP: %s:%s, IMAP: %s:%s, Port web: %s, Expéditeur: %s", config.SMTPHost, config.SMTPPort, config.IMAPHost, config.IMAPPort, config.Port, config.SenderName)
	log.Printf("[INFO] Délai entre envois: %d ms, Fichier CSV: %s", config.SendDelayMs, config.CSVPath)

	if *webMode {
		// Démarrer en mode serveur Web
		runWebServer()
	} else {
		// Démarrer en mode envoi d'e-mails (par défaut - mode CLI/Cron)
		runEmailSender()
	}
}

// Thread-safe getters and setters pour la configuration active
func getActiveConfig() Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return activeConfig
}

func setActiveConfig(cfg Config) {
	configMutex.Lock()
	activeConfig = cfg
	configMutex.Unlock()
}

// loadSettings charge les paramètres depuis settings.json ou utilise les valeurs par défaut
func loadSettings() Settings {
	var s Settings
	
	cfg := getActiveConfig()
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

// saveSettings enregistre les paramètres dans settings.json
func saveSettings(s Settings) error {
	file, err := os.Create("settings.json")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(s)
}

// loadTemplates charge les modèles depuis templates.json ou importe les fichiers existants
func loadTemplates() []EmailTemplate {
	var templates []EmailTemplate
	file, err := os.Open("templates.json")
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&templates)
	}

	// Si aucun modèle n'est enregistré, on tente d'importer les fichiers existants
	if len(templates) == 0 {
		// Modèle HTML
		htmlBodyBytes, errHtml := os.ReadFile("template.html")
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
		txtBodyBytes, errTxt := os.ReadFile("template.txt")
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
		saveTemplates(templates)
	}
	return templates
}

// saveTemplates enregistre les modèles dans templates.json
func saveTemplates(templates []EmailTemplate) error {
	file, err := os.Create("templates.json")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(templates)
}

var sentHistoryMutex sync.Mutex

// loadSentHistory charge l'historique d'envoi depuis sent_history.json
func loadSentHistory() []SentRecord {
	sentHistoryMutex.Lock()
	defer sentHistoryMutex.Unlock()

	var history []SentRecord
	file, err := os.Open("sent_history.json")
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&history)
	}
	if history == nil {
		history = []SentRecord{}
	}
	return history
}

// saveSentRecord ajoute un nouvel envoi dans l'historique sent_history.json
func saveSentRecord(rec SentRecord) error {
	sentHistoryMutex.Lock()
	defer sentHistoryMutex.Unlock()

	var history []SentRecord
	file, err := os.Open("sent_history.json")
	if err == nil {
		json.NewDecoder(file).Decode(&history)
		file.Close()
	}
	if history == nil {
		history = []SentRecord{}
	}
	history = append(history, rec)

	file, err = os.Create("sent_history.json")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(history)
}

// getAttachmentsList lit le dossier attachments/ et retourne la liste des noms de fichiers
func getAttachmentsList() ([]string, error) {
	err := os.MkdirAll("attachments", 0755)
	if err != nil {
		return nil, err
	}

	files, err := os.ReadDir("attachments")
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

// getAttachmentsListBytes lit les fichiers du dossier attachments/ et les renvoie sous forme d'octets
func getAttachmentsListBytes() ([]Attachment, error) {
	dirEntries, err := os.ReadDir("attachments")
	if err != nil {
		return nil, err
	}

	var list []Attachment
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join("attachments", entry.Name())
		b, errRead := os.ReadFile(path)
		if errRead != nil {
			log.Printf("[WARNING] Impossible de lire la pièce jointe %s : %v", entry.Name(), errRead)
			continue
		}
		list = append(list, Attachment{
			Name:  entry.Name(),
			Bytes: b,
		})
	}
	return list, nil
}

// writeEnvFile écrit un nouveau fichier .env propre
func writeEnvFile(vars EnvData) error {
	file, err := os.Create(".env")
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	fmt.Fprintf(writer, "# Configuration du Serveur SMTP (Envoi)\n")
	fmt.Fprintf(writer, "SMTP_HOST=%s\n", vars.SMTPHost)
	fmt.Fprintf(writer, "SMTP_PORT=%s\n", vars.SMTPPort)
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "# Configuration du Serveur IMAP (Réception/Suivi)\n")
	fmt.Fprintf(writer, "IMAP_HOST=%s\n", vars.IMAPHost)
	fmt.Fprintf(writer, "IMAP_PORT=%s\n", vars.IMAPPort)
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "# Configuration du Serveur Web (Dashboard)\n")
	fmt.Fprintf(writer, "PORT=%s\n", vars.Port)
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "# Identifiants de Connexion (Gmail)\n")
	fmt.Fprintf(writer, "SMTP_EMAIL=%s\n", vars.SMTPEmail)
	fmt.Fprintf(writer, "SMTP_PASSWORD=%s\n", vars.SMTPPassword)
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "# Configuration de l'Expéditeur\n")
	fmt.Fprintf(writer, "SENDER_NAME=%s\n", vars.SenderName)
	fmt.Fprintf(writer, "CSV_PATH=recipients.csv\n")
	fmt.Fprintf(writer, "TEMPLATE_PATH=template.txt\n")
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "# Délai entre chaque envoi (en ms)\n")
	fmt.Fprintf(writer, "SEND_DELAY_MS=%s\n", vars.SendDelayMs)

	return writer.Flush()
}

// runEmailSender gère la campagne d'envoi d'e-mails (CLI / Cron)
func runEmailSender() {
	cfg := getActiveConfig()
	log.Println("[INFO] Mode Envoi CLI : Démarrage de la campagne d'e-mails...")

	// Charger les paramètres
	settings := loadSettings()

	// Charger les pièces jointes
	attachments, err := getAttachmentsListBytes()
	if err != nil {
		log.Fatalf("[FATAL] Impossible de lire les pièces jointes : %v", err)
	}

	// Charger les modèles et utiliser le premier modèle par défaut
	templatesList := loadTemplates()
	var selectedTemplate *EmailTemplate
	if len(templatesList) > 0 {
		selectedTemplate = &templatesList[0]
	}

	if selectedTemplate == nil {
		log.Fatalf("[FATAL] Aucun modèle d'e-mail disponible.")
	}

	isHTML := selectedTemplate.Type == "html"
	bodyText := preprocessTemplateBody(selectedTemplate.Body, settings.Links)
	tmpl, err := template.New("email").Parse(bodyText)
	if err != nil {
		log.Fatalf("[FATAL] Impossible de parser le template %s : %v", selectedTemplate.Name, err)
	}

	// Charger les destinataires
	recipients, err := loadRecipients(cfg.CSVPath)
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
		email, exists := recipient["Email"]
		if !exists || email == "" {
			log.Printf("[WARNING] Ligne %d sautée : adresse email manquante", i+2)
			failureCount++
			continue
		}

		if !isValidEmail(email) {
			log.Printf("[WARNING] Ligne %d sautée : format email invalide (%s)", i+2, email)
			failureCount++
			continue
		}

		log.Printf("[INFO] [%d/%d] Envoi du mail à : %s", i+1, len(recipients), email)

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
			log.Printf("[ERROR] Erreur template pour %s : %v", email, err)
			failureCount++
			continue
		}

		msg := buildMultipartMessage(cfg.SenderName, cfg.SMTPEmail, email, selectedTemplate.Subject, bodyBuffer.String(), attachments)
		addr := fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort)

		err = smtp.SendMail(addr, auth, cfg.SMTPEmail, []string{email}, msg)
		if err != nil {
			log.Printf("[ERROR] Échec de l'envoi à %s : %v", email, err)
			failureCount++
		} else {
			log.Printf("[SUCCESS] E-mail envoyé à %s", email)
			successCount++
			saveSentRecord(SentRecord{
				Email:      email,
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

// runCampaignBackground exécute l'envoi de la campagne en arrière-plan (mode Web)
func runCampaignBackground(templateId string) {
	defer func() {
		sendMutex.Lock()
		sendInProgress = false
		sendMutex.Unlock()
	}()

	cfg := getActiveConfig()
	settings := loadSettings()
	
	attachments, err := getAttachmentsListBytes()
	if err != nil {
		sendMutex.Lock()
		sendLastLog = "Erreur lecture pièces jointes : " + err.Error()
		sendMutex.Unlock()
		return
	}

	// Charger les modèles
	templatesList := loadTemplates()
	var selectedTemplate *EmailTemplate
	for i := range templatesList {
		if templatesList[i].ID == templateId {
			selectedTemplate = &templatesList[i]
			break
		}
	}

	// Fallback si non trouvé
	if selectedTemplate == nil && len(templatesList) > 0 {
		selectedTemplate = &templatesList[0]
	}

	if selectedTemplate == nil {
		sendMutex.Lock()
		sendLastLog = "Erreur : Aucun modèle d'e-mail disponible."
		sendMutex.Unlock()
		return
	}

	isHTML := selectedTemplate.Type == "html"
	bodyText := preprocessTemplateBody(selectedTemplate.Body, settings.Links)
	tmpl, err := template.New("email").Parse(bodyText)
	if err != nil {
		sendMutex.Lock()
		sendLastLog = "Erreur syntaxe modèle : " + err.Error()
		sendMutex.Unlock()
		return
	}

	recipients, err := loadRecipients(cfg.CSVPath)
	if err != nil {
		sendMutex.Lock()
		sendLastLog = "Erreur chargement CSV : " + err.Error()
		sendMutex.Unlock()
		return
	}

	if len(recipients) == 0 {
		sendMutex.Lock()
		sendLastLog = "Aucun destinataire dans le fichier CSV."
		sendMutex.Unlock()
		return
	}

	sendMutex.Lock()
	sendTotal = len(recipients)
	sendCurrent = 0
	sendSuccessCount = 0
	sendFailureCount = 0
	sendLastLog = "Début de la campagne d'envoi en arrière-plan...\n"
	sendMutex.Unlock()

	auth := smtp.PlainAuth("", cfg.SMTPEmail, cfg.SMTPPassword, cfg.SMTPHost)

	for i, recipient := range recipients {
		email := recipient["Email"]
		if email == "" || !isValidEmail(email) {
			sendMutex.Lock()
			sendCurrent = i + 1
			sendFailureCount++
			if email == "" {
				sendLastLog += fmt.Sprintf("[%d/%d] Ligne %d sautée : adresse email manquante\n", i+1, sendTotal, i+2)
			} else {
				sendLastLog += fmt.Sprintf("[%d/%d] Ligne %d sautée : format email invalide (%s)\n", i+1, sendTotal, i+2, email)
			}
			sendMutex.Unlock()
			continue
		}

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
			sendMutex.Lock()
			sendCurrent = i + 1
			sendFailureCount++
			sendLastLog += fmt.Sprintf("[%d/%d] Erreur template pour %s: %v\n", i+1, sendTotal, email, err)
			sendMutex.Unlock()
			continue
		}

		msg := buildMultipartMessage(cfg.SenderName, cfg.SMTPEmail, email, selectedTemplate.Subject, bodyBuffer.String(), attachments)
		addr := fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort)

		err = smtp.SendMail(addr, auth, cfg.SMTPEmail, []string{email}, msg)
		
		sendMutex.Lock()
		sendCurrent = i + 1
		if err != nil {
			sendFailureCount++
			sendLastLog += fmt.Sprintf("[%d/%d] ÉCHEC de l'envoi à %s: %v\n", i+1, sendTotal, email, err)
		} else {
			sendSuccessCount++
			sendLastLog += fmt.Sprintf("[%d/%d] SUCCÈS de l'envoi à %s\n", i+1, sendTotal, email)
			saveSentRecord(SentRecord{
				Email:      email,
				Subject:    selectedTemplate.Subject,
				Date:       time.Now(),
				TemplateID: selectedTemplate.ID,
			})
		}
		sendMutex.Unlock()

		if i < len(recipients)-1 && cfg.SendDelayMs > 0 {
			time.Sleep(time.Duration(cfg.SendDelayMs) * time.Millisecond)
		}
	}

	sendMutex.Lock()
	sendLastLog += fmt.Sprintf("\nCampagne terminée. Succès : %d | Échecs : %d", sendSuccessCount, sendFailureCount)
	sendMutex.Unlock()
}

// runWebServer lance le serveur web HTTP
func runWebServer() {
	cfg := getActiveConfig()
	log.Printf("[INFO] Mode Serveur Web : Lancement sur http://0.0.0.0:%s", cfg.Port)
	log.Printf("[INFO] Chargement des données...")

	// Route principale (serve le dashboard)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		log.Println("[INFO] Rendu du tableau de bord...")
		start := time.Now()

		cfg := getActiveConfig()

		// Charger la liste des destinataires
		recipients, err := loadRecipients(cfg.CSVPath)
		if err != nil {
			log.Printf("[ERROR] Échec chargement CSV (%s) : %v", cfg.CSVPath, err)
			http.Error(w, fmt.Sprintf("Erreur chargement CSV : %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("[INFO] %d destinataires chargés depuis %s", len(recipients), cfg.CSVPath)

		// Charger les réponses synchronisées
		replies, err := loadReplies("replies.json")
		if err != nil {
			log.Printf("[WARNING] Impossible de charger replies.json : %v", err)
			replies = []Reply{}
		} else {
			log.Printf("[INFO] %d réponses chargées depuis replies.json", len(replies))
		}

		// Charger les paramètres (sujet, portfolio)
		settings := loadSettings()
		log.Printf("[INFO] Paramètres chargés — Sujet: %s, Portfolio: %s, Liens: %d", settings.Subject, settings.PortfolioURL, len(settings.Links))

		// Charger la liste des pièces jointes actuelles
		attachments, err := getAttachmentsList()
		if err != nil {
			log.Printf("[WARNING] Impossible de lister les pièces jointes : %v", err)
			attachments = []string{}
		} else {
			log.Printf("[INFO] %d pièce(s) jointe(s) disponible(s)", len(attachments))
		}

		// Charger l'historique global des envois
		sentHistory := loadSentHistory()
		log.Printf("[INFO] %d envoi(s) dans l'historique", len(sentHistory))

		// Charger les modèles
		templatesList := loadTemplates()
		log.Printf("[INFO] %d modèle(s) d'e-mail chargé(s)", len(templatesList))

		// Préparer les items du dashboard
		var items []DashboardItem
		totalReplies := 0

		for _, rec := range recipients {
			email := rec["Email"]
			if email == "" {
				continue
			}

			var latestReply *Reply
			for i := range replies {
				rep := &replies[i]
				if strings.ToLower(rep.Email) == strings.ToLower(email) {
					if latestReply == nil || rep.Date.After(latestReply.Date) {
						latestReply = rep
					}
				}
			}

			// Filtrer les envois pour cet email
			var candidateSentHistory []SentEvent
			for _, sh := range sentHistory {
				if strings.ToLower(sh.Email) == strings.ToLower(email) {
					candidateSentHistory = append(candidateSentHistory, SentEvent{
						Subject: sh.Subject,
						Date:    sh.Date,
					})
				}
			}

			// Filtrer TOUTES les réponses pour cet email (triées par date croissante)
			var candidateReplies []ReplyEvent
			for _, rep := range replies {
				if strings.ToLower(rep.Email) == strings.ToLower(email) {
					candidateReplies = append(candidateReplies, ReplyEvent{
						Subject: rep.Subject,
						Date:    rep.Date,
						Snippet: rep.Snippet,
						Body:    rep.Body,
					})
				}
			}
			// Trier les réponses par date croissante (du plus ancien au plus récent)
			for i := 0; i < len(candidateReplies)-1; i++ {
				for j := i + 1; j < len(candidateReplies); j++ {
					if candidateReplies[j].Date.Before(candidateReplies[i].Date) {
						candidateReplies[i], candidateReplies[j] = candidateReplies[j], candidateReplies[i]
					}
				}
			}

			item := DashboardItem{
				Email:       email,
				FirstName:   rec["FirstName"],
				LastName:    rec["LastName"],
				Company:     rec["Company"],
				Position:    rec["Position"],
				SentCount:   len(candidateSentHistory),
				SentHistory: candidateSentHistory,
			}

			if len(candidateReplies) > 0 {
				latestReply := candidateReplies[len(candidateReplies)-1]
				item.HasReply = true
				item.ReplyCount = len(candidateReplies)
				item.ReplySubject = latestReply.Subject
				item.ReplySnippet = latestReply.Snippet
				item.ReplyDate = latestReply.Date.Format("02/01/2006 15:04")
				item.ReplyHistory = candidateReplies
				totalReplies += len(candidateReplies)
			}

			items = append(items, item)
		}

		totalSent := len(sentHistory)
		totalWaiting := totalSent - totalReplies
		if totalWaiting < 0 {
			totalWaiting = 0
		}
		replyRate := 0
		if totalSent > 0 {
			replyRate = (totalReplies * 100) / totalSent
		}

		log.Printf("[INFO] Stats dashboard — Envoiés: %d, Réponses: %d, En attente: %d, Taux: %d%%", totalSent, totalReplies, totalWaiting, replyRate)

		// Préparer la structure des variables .env pour l'interface
		envData := EnvData{
			SMTPHost:     cfg.SMTPHost,
			SMTPPort:     cfg.SMTPPort,
			IMAPHost:     cfg.IMAPHost,
			IMAPPort:     cfg.IMAPPort,
			Port:         cfg.Port,
			SMTPEmail:    cfg.SMTPEmail,
			SMTPPassword: cfg.SMTPPassword,
			SenderName:   cfg.SenderName,
			SendDelayMs:  strconv.Itoa(cfg.SendDelayMs),
		}

		// Sérialisations JSON sécurisées pour injection JS dans Vue
		itemsBytes, _ := json.Marshal(items)
		repliesBytes, _ := json.Marshal(replies)
		settingsBytes, _ := json.Marshal(settings)
		templatesBytes, _ := json.Marshal(templatesList)
		attachmentsBytes, _ := json.Marshal(attachments)
		envBytes, _ := json.Marshal(envData)

		data := DashboardPageData{
			TotalSent:       totalSent,
			TotalReplies:    totalReplies,
			TotalWaiting:    totalWaiting,
			ReplyRate:       replyRate,
			ItemsJSON:       template.JS(itemsBytes),
			RepliesJSON:     template.JS(repliesBytes),
			SettingsJSON:    template.JS(settingsBytes),
			TemplateJSON:    template.JS(settingsBytes), // legacy
			TemplatesJSON:   template.JS(templatesBytes),
			AttachmentsJSON: template.JS(attachmentsBytes),
			EnvJSON:         template.JS(envBytes),
		}

		tmpl, err := template.ParseFiles("web/dashboard.html")
		if err != nil {
			log.Printf("[ERROR] Échec chargement web/dashboard.html : %v", err)
			http.Error(w, fmt.Sprintf("Erreur chargement web/dashboard.html : %v", err), http.StatusInternalServerError)
			return
		}

		err = tmpl.Execute(w, data)
		if err != nil {
			log.Printf("[ERROR] Erreur rendu template : %v", err)
		}
		log.Printf("[INFO] Tableau de bord rendu en %s", time.Since(start))
	})

	// Route pour enregistrer les paramètres et le template brut
	http.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Subject      string          `json:"subject"`
			PortfolioURL string          `json:"portfolioUrl"`
			Links        []Link          `json:"links"`
			Templates    []EmailTemplate `json:"templates"`
			TemplateHTML string          `json:"templateHtml"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			log.Printf("[ERROR] Requête /settings JSON invalide : %v", err)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
			return
		}
		log.Printf("[INFO] Sauvegarde des paramètres — Sujet: %s, %d liens, %d modèles", req.Subject, len(req.Links), len(req.Templates))

		// Sauvegarder les paramètres dans settings.json
		settings := Settings{
			Subject:      req.Subject,
			PortfolioURL: req.PortfolioURL,
			Links:        req.Links,
		}
		err = saveSettings(settings)
		if err != nil {
			log.Printf("[ERROR] Échec sauvegarde settings.json : %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Sauvegarde paramètres échouée"})
			return
		}
		log.Println("[SUCCESS] settings.json sauvegardé")

		// Sauvegarder les modèles dans templates.json
		err = saveTemplates(req.Templates)
		if err != nil {
			log.Printf("[ERROR] Échec sauvegarde templates.json : %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Sauvegarde modèles échouée"})
			return
		}
		log.Println("[SUCCESS] templates.json sauvegardé")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// Route pour enregistrer la liste des destinataires CSV
	http.HandleFunc("/save-recipients", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		var req []struct {
			Email     string `json:"Email"`
			FirstName string `json:"FirstName"`
			LastName  string `json:"LastName"`
			Company   string `json:"Company"`
			Position  string `json:"Position"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			log.Printf("[ERROR] Requête /save-recipients JSON invalide : %v", err)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
			return
		}
		log.Printf("[INFO] Sauvegarde de %d destinataires dans le CSV", len(req))

		cfg := getActiveConfig()
		file, err := os.Create(cfg.CSVPath)
		if err != nil {
			log.Printf("[ERROR] Impossible de créer le fichier CSV %s : %v", cfg.CSVPath, err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Impossible de créer le fichier CSV"})
			return
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		headers := []string{"email", "first_name", "last_name", "company", "position"}
		err = writer.Write(headers)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Impossible d'écrire les en-têtes"})
			return
		}

		for _, item := range req {
			record := []string{
				strings.TrimSpace(item.Email),
				strings.TrimSpace(item.FirstName),
				strings.TrimSpace(item.LastName),
				strings.TrimSpace(item.Company),
				strings.TrimSpace(item.Position),
			}
			err = writer.Write(record)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Erreur d'écriture d'une ligne"})
				return
			}
		}

		log.Printf("[SUCCESS] %d destinataires sauvegardés dans %s", len(req), cfg.CSVPath)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// Route pour supprimer un candidat, ses envois et ses réponses de l'historique
	http.HandleFunc("/delete-candidate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Email string `json:"email"`
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil || req.Email == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "E-mail invalide"})
			return
		}

		targetEmail := strings.ToLower(strings.TrimSpace(req.Email))
		log.Printf("[INFO] Suppression du candidat : %s", targetEmail)
		cfg := getActiveConfig()

		// 1. Supprimer du CSV
		recipients, err := loadRecipients(cfg.CSVPath)
		if err == nil {
			var updatedRecipients []map[string]string
			for _, rec := range recipients {
				if strings.ToLower(strings.TrimSpace(rec["Email"])) != targetEmail {
					updatedRecipients = append(updatedRecipients, rec)
				}
			}
			
			// Réécrire le CSV
			file, errCsv := os.Create(cfg.CSVPath)
			if errCsv == nil {
				writer := csv.NewWriter(file)
				headers := []string{"email", "first_name", "last_name", "company", "position"}
				_ = writer.Write(headers)
				for _, item := range updatedRecipients {
					_ = writer.Write([]string{
						strings.TrimSpace(item["Email"]),
						strings.TrimSpace(item["FirstName"]),
						strings.TrimSpace(item["LastName"]),
						strings.TrimSpace(item["Company"]),
						strings.TrimSpace(item["Position"]),
					})
				}
				writer.Flush()
				file.Close()
				log.Printf("[INFO] %d destinataire(s) restant(s) dans le CSV", len(updatedRecipients))
			}
		}

		// 2. Supprimer de sent_history.json
		sentHistory := loadSentHistory()
		updatedSentHistory := make([]SentRecord, 0)
		for _, sh := range sentHistory {
			if strings.ToLower(strings.TrimSpace(sh.Email)) != targetEmail {
				updatedSentHistory = append(updatedSentHistory, sh)
			}
		}
		
		// Réécrire sent_history.json (protégé par le mutex)
		sentHistoryMutex.Lock()
		fileSent, errSent := os.Create("sent_history.json")
		if errSent == nil {
			encoder := json.NewEncoder(fileSent)
			encoder.SetIndent("", "  ")
			_ = encoder.Encode(updatedSentHistory)
			fileSent.Close()
		}
		sentHistoryMutex.Unlock()
		log.Printf("[INFO] %d envoi(s) restant(s) dans l'historique", len(updatedSentHistory))

		// 3. Supprimer de replies.json
		replies, err := loadReplies("replies.json")
		if err == nil {
			updatedReplies := make([]Reply, 0)
			for _, rep := range replies {
				if strings.ToLower(strings.TrimSpace(rep.Email)) != targetEmail {
					updatedReplies = append(updatedReplies, rep)
				}
			}
			_ = saveReplies("replies.json", updatedReplies)
			log.Printf("[INFO] %d réponse(s) restante(s)", len(updatedReplies))
		}

		log.Printf("[SUCCESS] Candidat %s supprimé", targetEmail)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// Route pour modifier le fichier .env
	http.HandleFunc("/save-env", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		var req EnvData
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			log.Printf("[ERROR] Requête /save-env JSON invalide : %v", err)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
			return
		}
		log.Printf("[INFO] Mise à jour .env — SMTP: %s:%s, IMAP: %s:%s, Port: %s", req.SMTPHost, req.SMTPPort, req.IMAPHost, req.IMAPPort, req.Port)

		// 1. Écrire le fichier .env sur le disque
		err = writeEnvFile(req)
		if err != nil {
			log.Printf("[ERROR] Écriture .env impossible : %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Écriture .env impossible : " + err.Error()})
			return
		}

		// 2. Définir les nouvelles variables d'env dans le processus actuel
		os.Setenv("SMTP_HOST", req.SMTPHost)
		os.Setenv("SMTP_PORT", req.SMTPPort)
		os.Setenv("IMAP_HOST", req.IMAPHost)
		os.Setenv("IMAP_PORT", req.IMAPPort)
		os.Setenv("PORT", req.Port)
		os.Setenv("SMTP_EMAIL", req.SMTPEmail)
		os.Setenv("SMTP_PASSWORD", req.SMTPPassword)
		os.Setenv("SENDER_NAME", req.SenderName)
		os.Setenv("SEND_DELAY_MS", req.SendDelayMs)

		// 3. Recharger la config active en direct
		newCfg, err := loadConfig()
		if err != nil {
			log.Printf("[ERROR] Rechargement config impossible : %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Rechargement config impossible : " + err.Error()})
			return
		}

		setActiveConfig(newCfg)
		log.Println("[SUCCESS] Fichier .env mis à jour et rechargé en direct.")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// Route pour déclencher la campagne d'envoi d'e-mails
	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			TemplateID string `json:"templateId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		log.Printf("[INFO] Demande de campagne d'envoi — TemplateID: %s", req.TemplateID)

		sendMutex.Lock()
		if sendInProgress {
			sendMutex.Unlock()
			log.Println("[WARNING] Campagne déjà en cours, requête refusée")
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Une campagne d'envoi est déjà en cours."))
			return
		}
		sendInProgress = true
		sendMutex.Unlock()

		// Lancer la campagne en arrière-plan avec le template choisi
		go runCampaignBackground(req.TemplateID)

		log.Println("[INFO] Campagne d'envoi démarrée en arrière-plan")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Envoi démarré"})
	})

	// Route pour récupérer le statut d'avancement de l'envoi
	http.HandleFunc("/send-status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		sendMutex.Lock()
		defer sendMutex.Unlock()
		
		json.NewEncoder(w).Encode(map[string]interface{}{
			"inProgress":   sendInProgress,
			"total":        sendTotal,
			"current":      sendCurrent,
			"lastLog":      sendLastLog,
			"successCount": sendSuccessCount,
			"failureCount": sendFailureCount,
		})
	})

	// Route pour téléverser une pièce jointe générique
	http.HandleFunc("/upload-attachment", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Fichier trop volumineux"})
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Fichier introuvable"})
			return
		}
		defer file.Close()

		err = os.MkdirAll("attachments", 0755)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Dossier pièces jointes inaccessible"})
			return
		}

		safeFilename := filepath.Base(header.Filename)
		out, err := os.Create(filepath.Join("attachments", safeFilename))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Impossible d'écrire le fichier"})
			return
		}
		defer out.Close()

		written, err := io.Copy(out, file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Erreur d'enregistrement"})
			return
		}

		log.Printf("[SUCCESS] Pièce jointe uploadée : %s (%d octets)", safeFilename, written)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// Route pour supprimer une pièce jointe
	http.HandleFunc("/delete-attachment", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Filename string `json:"filename"`
		}

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
			return
		}

		safeFilename := filepath.Base(req.Filename)
		path := filepath.Join("attachments", safeFilename)

		err = os.Remove(path)
		if err != nil {
			log.Printf("[ERROR] Échec suppression pièce jointe %s : %v", safeFilename, err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Échec de la suppression"})
			return
		}

		log.Printf("[SUCCESS] Pièce jointe supprimée : %s", safeFilename)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// Route pour la synchronisation IMAP
	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
			return
		}

		cfg := getActiveConfig()
		w.Header().Set("Content-Type", "application/json")
		log.Printf("[INFO] Début synchronisation IMAP — %s:%s", cfg.IMAPHost, cfg.IMAPPort)

		syncedCount, err := syncReplies(cfg)
		if err != nil {
			log.Printf("[ERROR] Échec de synchronisation IMAP : %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}

		log.Printf("[SUCCESS] Synchronisation IMAP terminée — %d nouvelle(s) réponse(s)", syncedCount)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "success",
			"synced":  syncedCount,
			"message": fmt.Sprintf("Synchronisation effectuée. %d e-mail(s) importé(s).", syncedCount),
		})
	})

	// Route pour les fichiers statiques (ex: /web/style.css)
	http.Handle("/web/", logRequest(http.StripPrefix("/web/", http.FileServer(http.Dir("web")))))

	log.Printf("[INFO] Serveur web démarré sur http://0.0.0.0:%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, logRequest(http.DefaultServeMux)))
}

// logRequest est un middleware qui logue chaque requête HTTP
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Wrapper pour capturer le status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)
		log.Printf("[HTTP] %s %s %s → %d (%s)", r.Method, r.URL.Path, r.RemoteAddr, wrapped.statusCode, duration)
	})
}

// responseWriter wrapper pour capturer le status code HTTP
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// syncReplies se connecte en IMAP et cherche les réponses
func syncReplies(cfg Config) (int, error) {
	log.Printf("[INFO] Chargement des destinataires depuis %s...", cfg.CSVPath)
	recipients, err := loadRecipients(cfg.CSVPath)
	if err != nil {
		return 0, fmt.Errorf("erreur lecture CSV : %w", err)
	}

	recipientsMap := make(map[string]bool)
	for _, rec := range recipients {
		email := strings.ToLower(strings.TrimSpace(rec["Email"]))
		if email != "" {
			recipientsMap[email] = true
		}
	}

	if len(recipientsMap) == 0 {
		return 0, errors.New("aucun destinataire dans le fichier CSV")
	}
	log.Printf("[INFO] %d destinataires uniques à surveiller", len(recipientsMap))

	replies, err := loadReplies("replies.json")
	if err != nil {
		log.Printf("[WARNING] Impossible de charger replies.json : %v", err)
		replies = []Reply{}
	} else {
		log.Printf("[INFO] %d réponses déjà stockées localement", len(replies))
	}

	imapAddr := fmt.Sprintf("%s:%s", cfg.IMAPHost, cfg.IMAPPort)
	log.Printf("[INFO] Connexion IMAP à %s...", imapAddr)
	c, err := client.DialTLS(imapAddr, nil)
	if err != nil {
		return 0, fmt.Errorf("connexion TLS IMAP impossible : %w", err)
	}
	defer c.Logout()
	log.Println("[SUCCESS] Connexion TLS IMAP établie")

	if err := c.Login(cfg.SMTPEmail, cfg.SMTPPassword); err != nil {
		return 0, fmt.Errorf("authentification IMAP échouée : %w", err)
	}
	log.Printf("[SUCCESS] Authentification IMAP réussie (%s)", cfg.SMTPEmail)

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return 0, fmt.Errorf("sélection INBOX impossible : %w", err)
	}
	log.Printf("[INFO] INBOX sélectionnée — %d message(s) total", mbox.Messages)

	if mbox.Messages == 0 {
		log.Println("[INFO] INBOX vide, aucune synchronisation nécessaire")
		return 0, nil
	}

	startSeq := mbox.Messages - 149
	if startSeq < 1 {
		startSeq = 1
	}
	log.Printf("[INFO] Analyse des messages %d à %d...", startSeq, mbox.Messages)

	seqset := new(imap.SeqSet)
	seqset.AddRange(startSeq, mbox.Messages)

	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid}
	messagesChan := make(chan *imap.Message, 150)
	doneChan := make(chan error, 1)

	go func() {
		doneChan <- c.Fetch(seqset, items, messagesChan)
	}()

	var fetchedMsgs []*imap.Message
	for msg := range messagesChan {
		fetchedMsgs = append(fetchedMsgs, msg)
	}

	if err := <-doneChan; err != nil {
		return 0, fmt.Errorf("erreur de fetch d'enveloppes : %w", err)
	}
	log.Printf("[INFO] %d enveloppe(s) récupérée(s)", len(fetchedMsgs))

	newRepliesCount := 0
	analyzedCount := 0

	for i := len(fetchedMsgs) - 1; i >= 0; i-- {
		msg := fetchedMsgs[i]
		if msg.Envelope == nil || len(msg.Envelope.From) == 0 {
			continue
		}

		fromAddr := msg.Envelope.From[0]
		senderEmail := strings.ToLower(fromAddr.MailboxName + "@" + fromAddr.HostName)

		if !recipientsMap[senderEmail] {
			continue
		}
		analyzedCount++

		alreadySaved := false
		for _, r := range replies {
			if strings.ToLower(r.Email) == senderEmail && 
				r.Subject == msg.Envelope.Subject && 
				r.Date.Equal(msg.Envelope.Date) {
				alreadySaved = true
				break
			}
		}

		if alreadySaved {
			continue
		}

		var section imap.BodySectionName
		bodyItems := []imap.FetchItem{section.FetchItem()}
		bodySeqset := new(imap.SeqSet)
		bodySeqset.AddNum(msg.SeqNum)

		bodyChan := make(chan *imap.Message, 1)
		bodyDoneChan := make(chan error, 1)

		go func() {
			bodyDoneChan <- c.Fetch(bodySeqset, bodyItems, bodyChan)
		}()

		bodyMsg := <-bodyChan
		if err := <-bodyDoneChan; err != nil {
			log.Printf("[WARNING] Échec de récupération du corps pour %s: %v", senderEmail, err)
			continue
		}

		if bodyMsg == nil {
			continue
		}

		r := bodyMsg.GetBody(&section)
		if r == nil {
			continue
		}

		mr, err := mail.CreateReader(r)
		if err != nil {
			log.Printf("[WARNING] Échec parsing mail reader pour %s: %v", senderEmail, err)
			continue
		}

		var bodyText string
		var bodyHTML string

		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				log.Printf("[WARNING] Échec lecture part pour %s: %v", senderEmail, err)
				break
			}

			switch h := p.Header.(type) {
			case *mail.InlineHeader:
				contentType, _, _ := h.ContentType()
				b, _ := io.ReadAll(p.Body)
				if strings.HasPrefix(contentType, "text/html") {
					bodyHTML = string(b)
				} else if strings.HasPrefix(contentType, "text/plain") {
					bodyText = string(b)
				}
			}
		}

		finalBody := bodyText
		if bodyHTML != "" {
			finalBody = bodyHTML
		}

		if finalBody == "" {
			b, _ := io.ReadAll(r)
			finalBody = string(b)
		}

		newReply := Reply{
			Email:   senderEmail,
			Subject: msg.Envelope.Subject,
			Date:    msg.Envelope.Date,
			Body:    finalBody,
			Snippet: stripHTML(finalBody),
		}

		replies = append(replies, newReply)
		newRepliesCount++
		log.Printf("[INFO] Nouvelle réponse détectée — %s | Sujet: %s | Date: %s", senderEmail, msg.Envelope.Subject, msg.Envelope.Date.Format("02/01/2006 15:04"))
	}

	log.Printf("[INFO] %d message(s) analysé(s) provenant des destinataires, %d nouvelle(s) réponse(s)", analyzedCount, newRepliesCount)

	if newRepliesCount > 0 {
		err = saveReplies("replies.json", replies)
		if err != nil {
			return 0, fmt.Errorf("erreur d'écriture replies.json : %w", err)
		}
		log.Printf("[SUCCESS] %d nouvelle(s) réponse(s) sauvegardée(s) dans replies.json", newRepliesCount)
	}

	return newRepliesCount, nil
}

// loadReplies charge les réponses existantes
func loadReplies(filename string) ([]Reply, error) {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []Reply{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var replies []Reply
	err = json.NewDecoder(file).Decode(&replies)
	if err != nil {
		return nil, err
	}
	if replies == nil {
		replies = []Reply{}
	}
	return replies, nil
}

// saveReplies enregistre la liste des réponses
func saveReplies(filename string, replies []Reply) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(replies)
}

// stripHTML nettoie le code HTML d'une chaîne
func stripHTML(htmlStr string) string {
	var buf bytes.Buffer
	inTag := false
	for _, r := range htmlStr {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				buf.WriteRune(r)
			}
		}
	}
	s := strings.Join(strings.Fields(buf.String()), " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")

	if len(s) > 130 {
		return s[:130] + "..."
	}
	return s
}

// isValidEmail vérifie que l'adresse email a un format basique valide (contient @ et un point après @)
func isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	atIndex := strings.Index(email, "@")
	if atIndex < 1 {
		return false
	}
	domain := email[atIndex+1:]
	return strings.Contains(domain, ".") && !strings.HasSuffix(domain, ".")
}

// buildMultipartMessage construit un message MIME multipart contenant le corps TEXTE BRUT et les pièces jointes
func buildMultipartMessage(senderName, senderEmail, recipientEmail, subject, bodyText string, attachments []Attachment) []byte {
	var builder strings.Builder
	boundary := "candidature-multipart-boundary-12345"

	// Headers principaux
	builder.WriteString(fmt.Sprintf("From: %s <%s>\r\n", senderName, senderEmail))
	builder.WriteString(fmt.Sprintf("To: %s\r\n", recipientEmail))
	builder.WriteString(fmt.Sprintf("Subject: =?UTF-8?B?%s?=\r\n", encodeBase64(subject)))
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n", boundary))
	builder.WriteString("\r\n")

	// Section 1 : Corps du courriel (Texte brut ou HTML)
	builder.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	contentType := "text/plain; charset=UTF-8"
	if strings.Contains(bodyText, "<html") || strings.Contains(bodyText, "</html>") || strings.Contains(bodyText, "<div") {
		contentType = "text/html; charset=UTF-8"
	}
	builder.WriteString(fmt.Sprintf("Content-Type: %s\r\n", contentType))
	builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(bodyText)
	builder.WriteString("\r\n")

	// Section 2 : Les pièces jointes (fichiers multiples)
	for _, att := range attachments {
		contentType := "application/octet-stream"
		ext := strings.ToLower(filepath.Ext(att.Name))
		switch ext {
		case ".pdf":
			contentType = "application/pdf"
		case ".png":
			contentType = "image/png"
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".txt":
			contentType = "text/plain"
		}

		builder.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		builder.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", contentType, att.Name))
		builder.WriteString("Content-Transfer-Encoding: base64\r\n")
		builder.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", att.Name))
		builder.WriteString("\r\n")

		encoded := base64.StdEncoding.EncodeToString(att.Bytes)
		for i := 0; i < len(encoded); i += 76 {
			end := i + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			builder.WriteString(encoded[i:end] + "\r\n")
		}
		builder.WriteString("\r\n")
	}

	builder.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	return []byte(builder.String())
}

// encodeBase64 encode une chaîne en Base64
func encodeBase64(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

// loadEnv lit un fichier .env simple
func loadEnv(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

// loadConfig extrait la configuration
func loadConfig() (Config, error) {
	var cfg Config

	cfg.SMTPHost = getEnvOr("SMTP_HOST", "smtp.gmail.com")
	cfg.SMTPPort = getEnvOr("SMTP_PORT", "587")
	cfg.IMAPHost = getEnvOr("IMAP_HOST", "imap.gmail.com")
	cfg.IMAPPort = getEnvOr("IMAP_PORT", "993")
	cfg.Port = getEnvOr("PORT", "8080")
	cfg.SMTPEmail = os.Getenv("SMTP_EMAIL")
	cfg.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	cfg.SenderName = getEnvOr("SENDER_NAME", "Mailsender")
	cfg.CSVPath = getEnvOr("CSV_PATH", "recipients.csv")
	cfg.TemplatePath = getEnvOr("TEMPLATE_PATH", "template.txt")

	delayStr := getEnvOr("SEND_DELAY_MS", "1500")
	delay, err := strconv.Atoi(delayStr)
	if err != nil {
		delay = 1500
	}
	cfg.SendDelayMs = delay

	if cfg.SMTPEmail == "" {
		return cfg, errors.New("la variable d'environnement SMTP_EMAIL est obligatoire")
	}
	if cfg.SMTPPassword == "" {
		return cfg, errors.New("la variable d'environnement SMTP_PASSWORD est obligatoire")
	}

	return cfg, nil
}

// getEnvOr retourne la valeur de la variable d'env, ou la valeur par défaut
func getEnvOr(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}

// cleanHeader normalise un en-tête CSV en PascalCase
func cleanHeader(h string) string {
	h = strings.TrimSpace(h)
	h = strings.ToLower(h)
	r := strings.NewReplacer("_", " ", "-", " ", ".", " ")
	h = r.Replace(h)
	words := strings.Fields(h)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, "")
}

// loadRecipients lit le fichier CSV
func loadRecipients(path string) ([]map[string]string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("erreur lors de l'ouverture du fichier %s : %w", absPath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("erreur lors de la lecture du CSV : %w", err)
	}

	if len(records) == 0 {
		return nil, errors.New("le fichier CSV est vide")
	}

	headers := records[0]
	cleanedHeaders := make([]string, len(headers))
	for i, h := range headers {
		cleanedHeaders[i] = cleanHeader(h)
	}

	var recipients []map[string]string
	for _, record := range records[1:] {
		if len(record) == 0 || (len(record) == 1 && record[0] == "") {
			continue
		}

		row := make(map[string]string)
		for i := 0; i < len(cleanedHeaders); i++ {
			val := ""
			if i < len(record) {
				val = strings.TrimSpace(record[i])
			}
			row[cleanedHeaders[i]] = val
		}
		recipients = append(recipients, row)
	}

	return recipients, nil
}

// preprocessTemplateBody convertit proprement les accolades simples en double accolades
// sans altérer les accolades doubles déjà existantes.
func preprocessTemplateBody(body string, links []Link) string {
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
