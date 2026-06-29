# Mailsender - Guide pour Agents de Codage

Ce document décrit l'architecture, les conventions et les commandes essentielles du projet **Mailsender**. Il est destiné aux agents de codage IA qui interviennent sur ce dépôt.

---

## Vue d'ensemble du projet

**Mailsender** est une application Go légère conçue pour automatiser l'envoi de candidatures par e-mail à des recruteurs et suivre les réponses via un tableau de bord web interactif. Elle fonctionne en deux modes :

- **Mode CLI** (par défaut) : envoie les e-mails personnalisés à partir d'un fichier CSV, avec un délai configurable entre chaque envoi.
- **Mode Web** (`-web`) : démarre un serveur HTTP local affichant un tableau de bord Vue.js pour suivre les candidatures, gérer les modèles, les destinataires, les pièces jointes et synchroniser les réponses Gmail via IMAP.

Le projet est conçu pour un usage personnel (single-user), sans base de données — toutes les données persistent dans des fichiers JSON et CSV.

---

## Stack technique

| Couche | Technologie |
|--------|-------------|
| Langage | Go 1.26.4 |
| SMTP | `net/smtp` (bibliothèque standard) |
| IMAP | `github.com/emersion/go-imap` v1.2.1 |
| Parsing mail | `github.com/emersion/go-message` v0.18.2 |
| Serveur HTTP | `net/http` (bibliothèque standard) |
| Templating | `html/template` (bibliothèque standard) |
| Frontend | Vue 3 (CDN), Element Plus (CDN + thème sombre), CSS personnalisé |
| Stockage | Fichiers JSON et CSV (pas de base de données) |

---

## Structure des fichiers

```text
.
├── main.go                  # Application Go complète (~1700 lignes, single-file)
├── go.mod                   # Définition du module : mail-sender
├── go.sum                   # Checksums des dépendances
├── mailsender               # Binaire Linux pré-compilé
│
├── .env                     # Configuration privée (credentials Gmail)
├── .env.example             # Template de configuration
├── settings.json            # Sujet, URL portfolio, liens personnalisés
├── templates.json           # Modèles d'e-mail (HTML + texte)
├── recipients.csv           # Liste des recruteurs
├── sent_history.json        # Historique des envois
├── replies.json             # Réponses synchronisées via IMAP
│
├── template.html            # Modèle HTML par défaut (importé automatiquement)
├── template.txt             # Modèle texte par défaut (importé automatiquement)
│
├── attachments/             # Pièces jointes envoyées avec les e-mails
│   └── CV_2026_Enzo_Vignola.pdf
│
├── web/
│   ├── dashboard.html       # SPA Vue.js + Element Plus (~1650 lignes)
│   └── style.css            # Styles personnalisés (~175 lignes)
│
└── README.md                # Documentation utilisateur en français
```

**Remarque importante** : tout le code Go est contenu dans un seul fichier `main.go` (`package main`). Il n'y a aucune sous-package ni architecture modulaire.

---

## Configuration

### Fichier `.env`

Le fichier `.env` est obligatoire. Il est lu au démarrage par la fonction `loadEnv()`. Les variables suivantes sont requises :

| Variable | Description | Défaut |
|----------|-------------|--------|
| `SMTP_HOST` | Serveur SMTP sortant | `smtp.gmail.com` |
| `SMTP_PORT` | Port SMTP | `587` |
| `IMAP_HOST` | Serveur IMAP entrant | `imap.gmail.com` |
| `IMAP_PORT` | Port IMAP | `993` |
| `PORT` | Port du tableau de bord web | `8080` |
| `SMTP_EMAIL` | Adresse Gmail | *(obligatoire)* |
| `SMTP_PASSWORD` | Mot de passe d'application Gmail | *(obligatoire)* |
| `SENDER_NAME` | Nom affiché de l'expéditeur | `Mailsender` |
| `CSV_PATH` | Chemin du fichier CSV | `recipients.csv` |
| `TEMPLATE_PATH` | Chemin du template legacy | `template.txt` |
| `SEND_DELAY_MS` | Délai entre envois (ms) | `1500` |

> **Attention** : Gmail exige un **mot de passe d'application** (16 caractères) et l'**IMAP activé** dans les paramètres du compte. Le mot de passe principal du compte ne fonctionnera pas.

### Fichiers JSON de données

- **`settings.json`** : stocke le sujet des e-mails, l'URL du portfolio et une liste de liens personnalisés (clé, libellé, URL).
- **`templates.json`** : stocke les modèles d'e-mail avec `id`, `name`, `subject`, `body`, `type` (`html` ou `txt`).
- **`sent_history.json`** : historique des envois (`Email`, `Subject`, `Date`, `TemplateID`).
- **`replies.json`** : réponses synchronisées via IMAP (`Email`, `Subject`, `Date`, `Body`, `Snippet`).

---

## Commandes de build et d'exécution

### Build

```bash
go build -o mailsender
```

Le binaire `mailsender` est déjà présent à la racine et compilé pour Linux.

### Exécution

**Mode CLI** (envoi immédiat des e-mails) :

```bash
./mailsender
```

**Mode Web** (tableau de bord) :

```bash
./mailsender -web
# Puis ouvrir http://localhost:8080
```

### Planification Cron (exemple du README)

```cron
30 8 * * 1,4 cd /home/nzoy/Scripts/Mailsender && ./mailsender >> mailsender.log 2>&1
```

---

## Architecture du code

### Organisation

Le projet adopte une architecture **single-file** : tout le code Go réside dans `main.go`. Il n'y a pas de packages séparés.

### Types principaux

- `Config` : configuration runtime (SMTP, IMAP, credentials, délais).
- `Settings` : paramètres modifiables via le web (sujet, portfolio, liens).
- `EmailTemplate` : modèle d'e-mail avec ID, nom, sujet, corps, type.
- `Reply` : réponse e-mail synchronisée via IMAP.
- `SentRecord` / `SentEvent` : enregistrement d'envoi.
- `DashboardItem` : agrégation des données CSV + dernière réponse + historique d'envois.
- `Attachment` : pièce jointe en mémoire (nom + octets).
- `EnvData` : structure pour l'édition du `.env` via l'interface web.
- `DashboardPageData` : structure injectée dans le template `dashboard.html`.

### Concurrency

L'application utilise des variables globales protégées par des mutex :

- `configMutex` (`sync.RWMutex`) : accès thread-safe à la configuration active.
- `sendMutex` (`sync.Mutex`) : état de la campagne d'envoi en arrière-plan.
- `sentHistoryMutex` (`sync.Mutex`) : accès au fichier `sent_history.json`.

### Points d'entrée

- `main()` : parse le flag `-web`, charge le `.env`, la config, puis branche vers `runWebServer()` ou `runEmailSender()`.
- `runEmailSender()` : mode CLI — envoie les e-mails à tous les destinataires du CSV.
- `runWebServer()` : mode web — enregistre les handlers HTTP et démarre le serveur.
- `runCampaignBackground(templateId string)` : exécute l'envoi dans une goroutine (appelée depuis le dashboard).

### Endpoints HTTP (mode web)

| Route | Méthode | Description |
|-------|---------|-------------|
| `/` | GET | Rend le tableau de bord (injection JSON dans Vue) |
| `/settings` | POST | Sauvegarde `settings.json` + `templates.json` |
| `/save-recipients` | POST | Réécrit `recipients.csv` |
| `/delete-candidate` | POST | Supprime un candidat du CSV, de l'historique et des réponses |
| `/save-env` | POST | Met à jour `.env` et recharge la config en live |
| `/send` | POST | Déclenche une campagne d'envoi (goroutine) |
| `/send-status` | GET | Polling du statut de la campagne en cours |
| `/upload-attachment` | POST | Upload d'un fichier dans `attachments/` |
| `/delete-attachment` | POST | Supprime un fichier de `attachments/` |
| `/sync` | POST | Synchronise les réponses IMAP |
| `/web/` | GET | Fichiers statiques (CSS) |

### Templating des e-mails

Les modèles utilisent la syntaxe Go `html/template`. Les variables disponibles sont :

- `{{.FirstName}}`, `{{.LastName}}`, `{{.Company}}`, `{{.Position}}`
- `{{.SenderName}}`, `{{.PortfolioURL}}`
- `{{.Links.Portfolio}}`, `{{.Links.Github}}`, etc. (définis dans `settings.json`)

La fonction `preprocessTemplateBody()` corrige les accolades simples `{.Var}` en doubles `{{.Var}}` pour éviter les erreurs de syntaxe.

### Frontend (dashboard.html)

- **Vue 3** avec Composition API.
- **Délimiteurs personnalisés** : `{[`, `]}` (pour éviter le conflit avec `{{ }}` de Go templates).
- **Element Plus** en mode sombre (`<html class="dark">`).
- **Injection de données** : le serveur Go sérialise les données en JSON et les injecte directement dans le HTML via `template.JS`.
- **Onglets** : Suivi (tracker), Destinataires (CSV), Modèle & Pièces jointes, Configuration .env.
- **Fonctionnalités** : statistiques, tableau filtrable, panneau latéral de détails, éditeur inline CSV, gestion multi-modèles, upload de pièces jointes, synchronisation IMAP, envoi de campagne avec logs temps réel.

---

## Conventions de code

### Style

- Le code est entièrement en français (commentaires, noms de fonctions, logs, messages d'erreur).
- Les noms de fonctions utilisent le camelCase (`loadSettings`, `runWebServer`, `buildMultipartMessage`).
- Les types exportés utilisent le PascalCase (`Config`, `EmailTemplate`, `DashboardItem`).
- Les logs utilisent un préfixe de niveau : `[INFO]`, `[WARNING]`, `[ERROR]`, `[FATAL]`, `[SUCCESS]`.

### Patterns

- **JSON-as-database** : tous les fichiers de données sont écrits avec `encoder.SetIndent("", "  ")` pour un formatage lisible.
- **CSV avec headers flexibles** : la fonction `cleanHeader()` normalise les en-têtes CSV en PascalCase (`first_name` → `FirstName`).
- **Migration automatique** : au premier démarrage, `template.html` et `template.txt` sont importés dans `templates.json` s'il est vide.
- **Fallbacks** : le code contient de nombreux fallbacks (template par défaut, historique vide, etc.) pour éviter les crashs.

---

## Tests

**Il n'existe aucun test automatisé dans ce projet.**

- Pas de fichiers `*_test.go`.
- Pas de framework de test dans les dépendances.
- Pas de CI/CD ni de scripts de build.

Le testing est entièrement manuel : exécution CLI, vérification via le tableau de bord, et synchronisation IMAP.

---

## Considérations de sécurité

- **`.env` est sensible** : il contient le mot de passe d'application Gmail. Ne jamais le commiter.
- **Pas d'authentification** sur le tableau de bord web — il est conçu pour un usage local uniquement.
- **HTTP uniquement** : pas de HTTPS (localhost).
- **Protection contre les traversées de répertoire** : `filepath.Base()` est utilisé lors du traitement des noms de fichiers uploadés.
- **Pas de retry** : en cas d'échec d'envoi SMTP, l'e-mail est simplement loggué comme échec. Pas de mécanisme de file d'attente ou de retry.
- **État en mémoire** : l'état de la campagne d'envoi (`sendInProgress`, `sendTotal`, etc.) est perdu en cas de redémarrage du serveur.

---

## Déploiement et runtime

### Prérequis

- Linux (le binaire est compilé pour Linux).
- Accès réseau à `smtp.gmail.com:587` et `imap.gmail.com:993`.
- Compte Gmail avec **2FA activé** et **mot de passe d'application** généré.
- **IMAP activé** dans les paramètres Gmail.

### Portabilité

- Binaire unique sans dépendances runtime.
- Tous les chemins sont relatifs au répertoire de travail.
- Pas de conteneurisation Docker.

### Limitations connues

- Conçu pour un seul utilisateur (pas de multi-tenancy).
- Pas de file d'attente persistante pour les envois échoués.
- Le statut de la campagne est en mémoire uniquement (perdu au redémarrage).
- Le serveur web écoute sur toutes les interfaces (`:` + `PORT`), pas seulement localhost.
