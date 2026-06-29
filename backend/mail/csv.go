package mail

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

// LoadRecipients lit le fichier CSV
func LoadRecipients(path string) ([]map[string]string, error) {
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
