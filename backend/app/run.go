package app

import (
	"flag"
	"io"
	"log"
	"os"

	"github.com/ozneyy/mailternance/backend/config"
	"github.com/ozneyy/mailternance/backend/mail"
	"github.com/ozneyy/mailternance/backend/server"
)

// Run démarre l'application selon le mode choisi (CLI ou web)
func Run() {
	// S'assurer que le dossier logs/ existe et configurer les logs
	_ = os.MkdirAll("logs", 0755)
	logFile, err := os.OpenFile("logs/mailternance.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	} else {
		log.Printf("[WARNING] Impossible d'ouvrir logs/mailternance.log : %v", err)
	}

	// 1. Définir et analyser les arguments de la ligne de commande
	webMode := flag.Bool("web", false, "Démarrer le serveur web de suivi (dashboard)")
	flag.Parse()

	// 2. Charger le fichier .env
	config.LoadEnv(".env")

	// 3. Charger la configuration globale initiale
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Erreur de configuration initiale : %v", err)
	}
	config.SetActiveConfig(cfg)

	// 4. S'assurer que le dossier des pièces jointes existe dans web/
	_ = os.MkdirAll("web/attachments", 0755)

	log.Printf("[INFO] Configuration chargée — SMTP: %s:%s, IMAP: %s:%s, Port web: %s, Expéditeur: %s", cfg.SMTPHost, cfg.SMTPPort, cfg.IMAPHost, cfg.IMAPPort, cfg.Port, cfg.SenderName)
	log.Printf("[INFO] Délai entre envois: %d ms, Fichier CSV: %s", cfg.SendDelayMs, cfg.CSVPath)

	if *webMode {
		// Démarrer en mode serveur Web
		server.RunWebServer()
	} else {
		// Démarrer en mode envoi d'e-mails (par défaut - mode CLI/Cron)
		mail.RunEmailSender()
	}
}
