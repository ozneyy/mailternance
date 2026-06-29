package storage

import (
	"html/template"
	"time"
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
	ReplyCount   int          `json:"ReplyCount"`
	ReplySubject string
	ReplySnippet string
	ReplyDate    string
	SentCount    int          `json:"SentCount"`
	SentHistory  []SentEvent  `json:"SentHistory"`
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
