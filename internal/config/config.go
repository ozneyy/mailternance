package config

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/ozneyy/mailternance/internal/storage"
)

var (
	configMutex  sync.RWMutex
	activeConfig storage.Config
)

// GetActiveConfig retourne la configuration active de manière thread-safe
func GetActiveConfig() storage.Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return activeConfig
}

// SetActiveConfig définit la configuration active de manière thread-safe
func SetActiveConfig(cfg storage.Config) {
	configMutex.Lock()
	activeConfig = cfg
	configMutex.Unlock()
}

// LoadEnv lit un fichier .env simple et charge les variables d'environnement
func LoadEnv(filename string) {
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

// LoadConfig extrait la configuration depuis les variables d'environnement
func LoadConfig() (storage.Config, error) {
	var cfg storage.Config

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
