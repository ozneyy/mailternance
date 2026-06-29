package web

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
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

	"github.com/ozneyy/mailternance/internal/config"
	emailpkg "github.com/ozneyy/mailternance/internal/email"
	"github.com/ozneyy/mailternance/internal/imap"
	"github.com/ozneyy/mailternance/internal/storage"
)

var (
	sendMutex        sync.Mutex
	sendInProgress   bool
	sendTotal        int
	sendCurrent      int
	sendLastLog      string
	sendSuccessCount int
	sendFailureCount int
)

// responseWriter wrapper pour capturer le status code HTTP
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// logRequest est un middleware qui logue chaque requête HTTP
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		duration := time.Since(start)
		log.Printf("[HTTP] %s %s %s → %d (%s)", r.Method, r.URL.Path, r.RemoteAddr, wrapped.statusCode, duration)
	})
}

// RunWebServer lance le serveur web HTTP
func RunWebServer() {
	cfg := config.GetActiveConfig()
	log.Printf("[INFO] Mode Serveur Web : Lancement sur http://0.0.0.0:%s", cfg.Port)
	log.Printf("[INFO] Chargement des données...")

	// Route principale (serve le dashboard)
	http.HandleFunc("/", handleDashboard)

	// Route pour enregistrer les paramètres et le template brut
	http.HandleFunc("/settings", handleSettings)

	// Route pour enregistrer la liste des destinataires CSV
	http.HandleFunc("/save-recipients", handleSaveRecipients)

	// Route pour supprimer un candidat, ses envois et ses réponses de l'historique
	http.HandleFunc("/delete-candidate", handleDeleteCandidate)

	// Route pour modifier le fichier .env
	http.HandleFunc("/save-env", handleSaveEnv)

	// Route pour déclencher la campagne d'envoi d'e-mails
	http.HandleFunc("/send", handleSend)

	// Route pour récupérer le statut d'avancement de l'envoi
	http.HandleFunc("/send-status", handleSendStatus)

	// Route pour téléverser une pièce jointe générique
	http.HandleFunc("/upload-attachment", handleUploadAttachment)

	// Route pour supprimer une pièce jointe
	http.HandleFunc("/delete-attachment", handleDeleteAttachment)

	// Route pour la synchronisation IMAP
	http.HandleFunc("/sync", handleSync)

	// Route pour les fichiers statiques (ex: /web/style.css)
	http.Handle("/web/", logRequest(http.StripPrefix("/web/", http.FileServer(http.Dir("assets/web")))))

	log.Printf("[INFO] Serveur web démarré sur http://0.0.0.0:%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, logRequest(http.DefaultServeMux)))
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	log.Println("[INFO] Rendu du tableau de bord...")
	start := time.Now()

	cfg := config.GetActiveConfig()

	// Charger la liste des destinataires
	recipients, err := emailpkg.LoadRecipients(cfg.CSVPath)
	if err != nil {
		log.Printf("[ERROR] Échec chargement CSV (%s) : %v", cfg.CSVPath, err)
		http.Error(w, fmt.Sprintf("Erreur chargement CSV : %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("[INFO] %d destinataires chargés depuis %s", len(recipients), cfg.CSVPath)

	// Charger les réponses synchronisées
	replies, err := storage.LoadReplies("replies.json")
	if err != nil {
		log.Printf("[WARNING] Impossible de charger replies.json : %v", err)
		replies = []storage.Reply{}
	} else {
		log.Printf("[INFO] %d réponses chargées depuis replies.json", len(replies))
	}

	// Charger les paramètres (sujet, portfolio)
	settings := storage.LoadSettings(cfg)
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
	sentHistory := storage.LoadSentHistory()
	log.Printf("[INFO] %d envoi(s) dans l'historique", len(sentHistory))

	// Charger les modèles
	templatesList := storage.LoadTemplates()
	log.Printf("[INFO] %d modèle(s) d'e-mail chargé(s)", len(templatesList))

	// Préparer les items du dashboard
	var items []storage.DashboardItem
	totalReplies := 0

	for _, rec := range recipients {
		email := rec["Email"]
		if email == "" {
			continue
		}

		var latestReply *storage.Reply
		for i := range replies {
			rep := &replies[i]
			if strings.ToLower(rep.Email) == strings.ToLower(email) {
				if latestReply == nil || rep.Date.After(latestReply.Date) {
					latestReply = rep
				}
			}
		}

		// Filtrer les envois pour cet email
		var candidateSentHistory []storage.SentEvent
		for _, sh := range sentHistory {
			if strings.ToLower(sh.Email) == strings.ToLower(email) {
				candidateSentHistory = append(candidateSentHistory, storage.SentEvent{
					Subject: sh.Subject,
					Date:    sh.Date,
				})
			}
		}

		// Filtrer TOUTES les réponses pour cet email (triées par date croissante)
		var candidateReplies []storage.ReplyEvent
		for _, rep := range replies {
			if strings.ToLower(rep.Email) == strings.ToLower(email) {
				candidateReplies = append(candidateReplies, storage.ReplyEvent{
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

		item := storage.DashboardItem{
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
	envData := storage.EnvData{
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

	data := storage.DashboardPageData{
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

	tmpl, err := template.ParseFiles("assets/web/dashboard.html")
	if err != nil {
		log.Printf("[ERROR] Échec chargement assets/web/dashboard.html : %v", err)
		http.Error(w, fmt.Sprintf("Erreur chargement assets/web/dashboard.html : %v", err), http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		log.Printf("[ERROR] Erreur rendu template : %v", err)
	}
	log.Printf("[INFO] Tableau de bord rendu en %s", time.Since(start))
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Subject      string                  `json:"subject"`
		PortfolioURL string                  `json:"portfolioUrl"`
		Links        []storage.Link          `json:"links"`
		Templates    []storage.EmailTemplate `json:"templates"`
		TemplateHTML string                  `json:"templateHtml"`
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
	settings := storage.Settings{
		Subject:      req.Subject,
		PortfolioURL: req.PortfolioURL,
		Links:        req.Links,
	}
	err = storage.SaveSettings(settings)
	if err != nil {
		log.Printf("[ERROR] Échec sauvegarde settings.json : %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Sauvegarde paramètres échouée"})
		return
	}
	log.Println("[SUCCESS] settings.json sauvegardé")

	// Sauvegarder les modèles dans templates.json
	err = storage.SaveTemplates(req.Templates)
	if err != nil {
		log.Printf("[ERROR] Échec sauvegarde templates.json : %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Sauvegarde modèles échouée"})
		return
	}
	log.Println("[SUCCESS] templates.json sauvegardé")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleSaveRecipients(w http.ResponseWriter, r *http.Request) {
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

	cfg := config.GetActiveConfig()
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
}

func handleDeleteCandidate(w http.ResponseWriter, r *http.Request) {
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
	cfg := config.GetActiveConfig()

	// 1. Supprimer du CSV
	recipients, err := emailpkg.LoadRecipients(cfg.CSVPath)
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
	sentHistory := storage.LoadSentHistory()
	updatedSentHistory := make([]storage.SentRecord, 0)
	for _, sh := range sentHistory {
		if strings.ToLower(strings.TrimSpace(sh.Email)) != targetEmail {
			updatedSentHistory = append(updatedSentHistory, sh)
		}
	}
	_ = storage.OverwriteSentHistory(updatedSentHistory)
	log.Printf("[INFO] %d envoi(s) restant(s) dans l'historique", len(updatedSentHistory))

	// 3. Supprimer de replies.json
	replies, err := storage.LoadReplies("replies.json")
	if err == nil {
		updatedReplies := make([]storage.Reply, 0)
		for _, rep := range replies {
			if strings.ToLower(strings.TrimSpace(rep.Email)) != targetEmail {
				updatedReplies = append(updatedReplies, rep)
			}
		}
		_ = storage.SaveReplies("replies.json", updatedReplies)
		log.Printf("[INFO] %d réponse(s) restante(s)", len(updatedReplies))
	}

	log.Printf("[SUCCESS] Candidat %s supprimé", targetEmail)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleSaveEnv(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}

	var req storage.EnvData
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
	newCfg, err := config.LoadConfig()
	if err != nil {
		log.Printf("[ERROR] Rechargement config impossible : %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "Rechargement config impossible : " + err.Error()})
		return
	}

	config.SetActiveConfig(newCfg)
	log.Println("[SUCCESS] Fichier .env mis à jour et rechargé en direct.")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func handleSend(w http.ResponseWriter, r *http.Request) {
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
}

func handleSendStatus(w http.ResponseWriter, r *http.Request) {
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
}

func handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
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
}

func handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
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
}

func handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Méthode non autorisée", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.GetActiveConfig()
	w.Header().Set("Content-Type", "application/json")
	log.Printf("[INFO] Début synchronisation IMAP — %s:%s", cfg.IMAPHost, cfg.IMAPPort)

	syncedCount, err := imap.SyncReplies(cfg)
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
func getAttachmentsListBytes() ([]storage.Attachment, error) {
	dirEntries, err := os.ReadDir("attachments")
	if err != nil {
		return nil, err
	}

	var list []storage.Attachment
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
		list = append(list, storage.Attachment{
			Name:  entry.Name(),
			Bytes: b,
		})
	}
	return list, nil
}

// writeEnvFile écrit un nouveau fichier .env propre
func writeEnvFile(vars storage.EnvData) error {
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

// runCampaignBackground exécute l'envoi de la campagne en arrière-plan (mode Web)
func runCampaignBackground(templateId string) {
	defer func() {
		sendMutex.Lock()
		sendInProgress = false
		sendMutex.Unlock()
	}()

	cfg := config.GetActiveConfig()
	settings := storage.LoadSettings(cfg)

	attachments, err := getAttachmentsListBytes()
	if err != nil {
		sendMutex.Lock()
		sendLastLog = "Erreur lecture pièces jointes : " + err.Error()
		sendMutex.Unlock()
		return
	}

	// Charger les modèles
	templatesList := storage.LoadTemplates()
	var selectedTemplate *storage.EmailTemplate
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
	bodyText := emailpkg.PreprocessTemplateBody(selectedTemplate.Body, settings.Links)
	tmpl, err := template.New("email").Parse(bodyText)
	if err != nil {
		sendMutex.Lock()
		sendLastLog = "Erreur syntaxe modèle : " + err.Error()
		sendMutex.Unlock()
		return
	}

	recipients, err := emailpkg.LoadRecipients(cfg.CSVPath)
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
		emailAddr := recipient["Email"]
		if emailAddr == "" || !emailpkg.IsValidEmail(emailAddr) {
			sendMutex.Lock()
			sendCurrent = i + 1
			sendFailureCount++
			if emailAddr == "" {
				sendLastLog += fmt.Sprintf("[%d/%d] Ligne %d sautée : adresse email manquante\n", i+1, sendTotal, i+2)
			} else {
				sendLastLog += fmt.Sprintf("[%d/%d] Ligne %d sautée : format email invalide (%s)\n", i+1, sendTotal, i+2, emailAddr)
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
			sendLastLog += fmt.Sprintf("[%d/%d] Erreur template pour %s: %v\n", i+1, sendTotal, emailAddr, err)
			sendMutex.Unlock()
			continue
		}

		msg := emailpkg.BuildMultipartMessage(cfg.SenderName, cfg.SMTPEmail, emailAddr, selectedTemplate.Subject, bodyBuffer.String(), attachments)
		addr := fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort)

		err = emailpkg.SendEmail(addr, auth, cfg.SMTPEmail, []string{emailAddr}, msg)

		sendMutex.Lock()
		sendCurrent = i + 1
		if err != nil {
			sendFailureCount++
			sendLastLog += fmt.Sprintf("[%d/%d] ÉCHEC de l'envoi à %s: %v\n", i+1, sendTotal, emailAddr, err)
		} else {
			sendSuccessCount++
			sendLastLog += fmt.Sprintf("[%d/%d] SUCCÈS de l'envoi à %s\n", i+1, sendTotal, emailAddr)
			storage.SaveSentRecord(storage.SentRecord{
				Email:      emailAddr,
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
