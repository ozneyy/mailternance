package mail

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/ozneyy/mailternance/backend/storage"
	"github.com/ozneyy/mailternance/backend/templates"
)

// SyncReplies se connecte en IMAP et cherche les réponses
func SyncReplies(cfg templates.Config) (int, error) {
	log.Printf("[INFO] Chargement des destinataires depuis %s...", cfg.CSVPath)
	recipients, err := LoadRecipients(cfg.CSVPath)
	if err != nil {
		return 0, fmt.Errorf("erreur lecture CSV : %w", err)
	}

	recipientsMap := make(map[string]bool)
	for _, rec := range recipients {
		e := strings.ToLower(strings.TrimSpace(rec["Email"]))
		if e != "" {
			recipientsMap[e] = true
		}
	}

	if len(recipientsMap) == 0 {
		return 0, errors.New("aucun destinataire dans le fichier CSV")
	}
	log.Printf("[INFO] %d destinataires uniques à surveiller", len(recipientsMap))

	replies, err := storage.LoadReplies("replies.json")
	if err != nil {
		log.Printf("[WARNING] Impossible de charger replies.json : %v", err)
		replies = []templates.Reply{}
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

		newReply := templates.Reply{
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
		err = storage.SaveReplies("replies.json", replies)
		if err != nil {
			return 0, fmt.Errorf("erreur d'écriture replies.json : %w", err)
		}
		log.Printf("[SUCCESS] %d nouvelle(s) réponse(s) sauvegardée(s) dans replies.json", newRepliesCount)
	}

	return newRepliesCount, nil
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
