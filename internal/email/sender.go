package email

import (
	"encoding/base64"
	"fmt"
	"net/smtp"
	"strings"

	"github.com/ozneyy/mailternance/internal/storage"
)

// IsValidEmail vérifie que l'adresse email a un format basique valide (contient @ et un point après @)
func IsValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	atIndex := strings.Index(email, "@")
	if atIndex < 1 {
		return false
	}
	domain := email[atIndex+1:]
	return strings.Contains(domain, ".") && !strings.HasSuffix(domain, ".")
}

// BuildMultipartMessage construit un message MIME multipart contenant le corps TEXTE BRUT et les pièces jointes
func BuildMultipartMessage(senderName, senderEmail, recipientEmail, subject, bodyText string, attachments []storage.Attachment) []byte {
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
		ext := strings.ToLower(strings.TrimPrefix(att.Name, "."))
		if ext == "" {
			ext = strings.ToLower(att.Name)
		}
		// Try to get extension from filepath
		if idx := strings.LastIndex(att.Name, "."); idx >= 0 {
			ext = strings.ToLower(att.Name[idx:])
		}
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

// SendEmail envoie un email via SMTP
func SendEmail(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, auth, from, to, msg)
}
