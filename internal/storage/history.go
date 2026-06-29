package storage

import (
	"encoding/json"
	"os"
	"sync"
)

var sentHistoryMutex sync.Mutex

// LoadSentHistory charge l'historique d'envoi depuis sent_history.json
func LoadSentHistory() []SentRecord {
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

// SaveSentRecord ajoute un nouvel envoi dans l'historique sent_history.json
func SaveSentRecord(rec SentRecord) error {
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

// OverwriteSentHistory réécrit l'historique d'envoi complet (utilisé pour la suppression de candidats)
func OverwriteSentHistory(history []SentRecord) error {
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

// LoadReplies charge les réponses existantes
func LoadReplies(filename string) ([]Reply, error) {
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

// SaveReplies enregistre la liste des réponses
func SaveReplies(filename string, replies []Reply) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(replies)
}
