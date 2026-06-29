# Mailternance

> **Suivi de candidatures alternance & envoi automatisé de emails**  
> Un outil auto-hébergé, léger et performant écrit en Go. Pas de base de données complexe, pas de SaaS tiers — vos données restent chez vous.

---

## ✨ Fonctionnalités

| Fonction | Description |
|----------|-------------|
| 📧 **Envoi automatisé** | Emails personnalisés via SMTP à partir d'un fichier CSV |
| 📊 **Dashboard web** | Interface sombre premium pour suivre candidatures et taux de réponse |
| 🔄 **Sync IMAP** | Récupération automatique des réponses recruteurs depuis Gmail |
| 📬 **Boîte de réception intégrée** | Lecture des réponses sans quitter l'application |
| ⏱️ **Rate limiting** | Délai configurable entre envois pour éviter le blocage |
| 🐳 **Docker-ready** | Déploiement en un clic avec Docker Compose |
| 🔒 **Zero dépendance lourde** | Stockage JSON local, pas de base de données |

---

## 🚀 Déploiement Rapide (Auto-hébergement)

### Prérequis

- [Docker](https://docs.docker.com/get-docker/) + Docker Compose
- Un compte Gmail avec **mot de passe d'application** ([guide Google](https://support.google.com/accounts/answer/185833))

### 1. Cloner le projet

```bash
git clone https://github.com/YOUR_USER/mailternance.git
cd mailternance
```

### 2. Configurer les credentials

```bash
cp .env.example .env
# Éditez .env avec vos vrais credentials Gmail
```

```ini
# .env
SMTP_EMAIL=votre.email@gmail.com
SMTP_PASSWORD=votre_mot_de_passe_app_16_caracteres
SENDER_NAME="Votre Prénom Nom"
```

> **⚠️ Important** : Gmail exige un [mot de passe d'application](https://myaccount.google.com/apppasswords) (validation 2 étapes requise). Votre mot de passe Gmail classique ne fonctionnera pas.

### 3. Préparer les destinataires

```bash
cp recipients.csv.example recipients.csv
# Éditez recipients.csv avec vos contacts
```

```csv
email,first_name,last_name,company,position
recruteur@entreprise.com,Jean,Dupont,TechCorp,Lead Dev
```

### 4. Lancer

```bash
docker compose up -d
```

Le dashboard est accessible sur : **http://localhost:17890**

---

## 📁 Structure du Projet

```
mailternance/
├── docker-compose.yml      # Orchestration Docker
├── Dockerfile              # Image optimisée multi-stage
├── .env.example            # Template de configuration
├── .env                    # ⚠️ Vos credentials (non commité)
├── main.go                 # Point d'entrée de l'application
├── backend/                # Code source Go ( logique métier & API )
│   ├── app/                # Initialisation et routage
│   ├── config/             # Chargement de configuration
│   ├── models/             # Modèles de données
│   ├── storage/            # Gestion des données locales
│   ├── mail/               # Envoi & Réception d'e-mails
│   └── server/             # Serveur web HTTP
├── web/                    # Ressources web du dashboard
│   ├── templates/          # Templates HTML et texte
│   ├── static/             # Assets statiques (CSS, JS...)
│   └── attachments/        # ⚠️ Pièces jointes (CV, etc., non commité)
├── logs/                   # Logs de l'application
├── recipients.csv          # ⚠️ Vos contacts (non commité)
├── sent_history.json       # Historique d'envoi
├── replies.json            # Réponses reçues
├── settings.json           # Paramètres runtime (sujet, liens...)
└── templates.json          # Modèles d'email configurables
```

> Les fichiers marqués ⚠️ sont ignorés par Git. Vos données personnelles ne quittent jamais votre machine.

---

## 🐳 Docker

### Image

- **Build multi-stage** : binaire compilé statiquement → image finale `debian:bookworm-slim` (~50 MB)
- **Volumes** : données persistantes montées depuis l'hôte
- **Healthcheck** : vérification automatique du dashboard

### Commandes utiles

```bash
# Démarrer
docker compose up -d

# Voir les logs
docker compose logs -f

# Redémarrer
docker compose restart

# Arrêter
docker compose down

# Mettre à jour (pull + rebuild)
docker compose pull && docker compose up -d --build
```

---

## ⚙️ Configuration

### Variables d'environnement (`.env`)

| Variable | Description | Défaut |
|----------|-------------|--------|
| `SMTP_HOST` | Serveur SMTP | `smtp.gmail.com` |
| `SMTP_PORT` | Port SMTP | `587` |
| `IMAP_HOST` | Serveur IMAP | `imap.gmail.com` |
| `IMAP_PORT` | Port IMAP | `993` |
| `SMTP_EMAIL` | Email d'envoi | *(obligatoire)* |
| `SMTP_PASSWORD` | Mot de passe d'application | *(obligatoire)* |
| `SENDER_NAME` | Nom de l'expéditeur | `Mailternance` |
| `PORT` | Port du dashboard | `17890` |
| `SEND_DELAY_MS` | Délai entre envois (ms) | `1500` |

### Templates d'email

Les variables disponibles dans `template.html` :

| Variable | Source |
|----------|--------|
| `{{.FirstName}}` | Colonne `first_name` du CSV |
| `{{.LastName}}` | Colonne `last_name` du CSV |
| `{{.Company}}` | Colonne `company` du CSV |
| `{{.Position}}` | Colonne `position` du CSV |
| `{{.PortfolioURL}}` | Paramètre `settings.json` |
| `{{.SenderName}}` | Variable d'env `SENDER_NAME` |

---

## 🔄 Modes d'utilisation

### Mode 1 : Envoi (CLI / Cron)

```bash
# En local
go build -o mailternance && ./mailternance

# En Docker (one-shot)
docker compose run --rm mailternance ./mailternance
```

**Planification cron** (lundi & jeudi à 8h30) :

```cron
30 8 * * 1,4 cd /chemin/vers/mailternance && docker compose run --rm mailternance ./mailternance >> cron.log 2>&1
```

### Mode 2 : Dashboard web

```bash
# En local
go build -o mailternance && ./mailternance -web

# En Docker (défaut)
docker compose up -d
```

Accès : **http://localhost:17890**

- **Synchroniser** : clic sur le bouton pour récupérer les nouvelles réponses
- **Panneau latéral** : clic sur un candidat pour lire sa réponse
- **Répondre** : clic sur "Répondre par email" pour ouvrir votre client mail

---

## 🔒 Sécurité & Vie Privée

- **Aucune donnée n'est envoyée à un tiers** — tout reste local
- **Credentials** : fichier `.env` jamais commité (`.gitignore`)
- **Contacts** : `recipients.csv` jamais commité
- **Historique** : `sent_history.json`, `replies.json` en local uniquement
- **SMTP/IMAP** : connexions TLS chiffrées

---

## 🛠️ Développement

### Prérequis

- Go 1.21+

### Build

```bash
go build -o mailternance
```

### Run

```bash
# Mode envoi
./mailternance

# Mode dashboard
./mailternance -web
```

---

## 📄 Licence

MIT — Voir [LICENSE](LICENSE)

---

> **Pourquoi auto-hébergé ?** Parce que vos candidatures, vos contacts et vos conversations avec les recruteurs ne méritent pas d'être stockées chez un prestataire tiers. Vos données, votre serveur.
