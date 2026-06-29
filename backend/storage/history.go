package storage

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/ozneyy/mailternance/backend/templates"
)

var sentHistoryMutex sync.Mutex

// LoadSentHistory charge l'historique d'envoi depuis sent_history.json ou retourne un tableau vide
func LoadSentHistory() []templates.SentRecord {
	sentHistoryMutex.Lock()
	defer sentHistoryMutex.Unlock()

	var history []templates.SentRecord
	file, err := os.Open("sent_history.json")
	if err == nil {
		defer file.Close()
		json.NewDecoder(file).Decode(&history)
	}
	if history == nil {
		history = []templates.SentRecord{}
	}
	return history
}

// SaveSentRecord ajoute un nouvel envoi dans l'historique sent_history.json
func SaveSentRecord(rec templates.SentRecord) error {
	sentHistoryMutex.Lock()
	defer sentHistoryMutex.Unlock()

	var history []templates.SentRecord
	file, err := os.Open("sent_history.json")
	if err == nil {
		json.NewDecoder(file).Decode(&history)
		file.Close()
	}
	if history == nil {
		history = []templates.SentRecord{}
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

// OverwriteSentHistory réécrit l'historique d'envoi complet (utilisé pour la suppression de candidats)
func OverwriteSentHistory(history []templates.SentRecord) error {
	sentHistoryMutex.Lock()
	defer sentHistoryMutex.Unlock()

	file, err := os.Create("sent_history.json")
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(history)
}

// LoadReplies charge les réponses existantes ou retourne un tableau vide si le fichier n'existe pas
func LoadReplies(filename string) ([]templates.Reply, error) {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []templates.Reply{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var replies []templates.Reply
	err = json.NewDecoder(file).Decode(&replies)
	if err != nil {
		return nil, err
	}
	if replies == nil {
		replies = []templates.Reply{}
	}
	return replies, nil
}

// SaveReplies enregistre la liste des réponses
func SaveReplies(filename string, replies []templates.Reply) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(replies)
}
