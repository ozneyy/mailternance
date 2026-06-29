# Mailsender - Suivi de Candidatures Alternance & Envoi Automatique

Cet outil léger et performant écrit en Go permet d'envoyer automatiquement des e-mails HTML de candidatures à partir d'un fichier CSV et de suivre les réponses des recruteurs dans un tableau de bord web interactif doté d'une interface CSS soignée.

## Fonctionnalités

*   **Envoi Automatique de Candidatures** : Envoi de mails personnalisés via SMTP Gmail à partir d'un fichier CSV.
*   **Tableau de Bord Web Interactif** : Interface premium en mode sombre pour suivre vos candidatures envoyées, voir le taux de réponse, et filtrer les contacts.
*   **Synchronisation IMAP** : Récupère automatiquement les réponses de vos recruteurs depuis votre boîte Gmail, associe chaque e-mail au bon candidat, et extrait un aperçu du message.
*   **Boîte de Réception Intégrée** : Affiche le corps de l'e-mail (HTML ou texte brut) reçu directement dans un panneau latéral sans quitter l'application.
*   **Gestion du débit (Rate Limiting)** : Espace l'envoi de chaque mail pour éviter le blocage de votre compte.
*   **Zéro Dépendance Lourde** : Pas de base de données complexe à installer, les réponses sont sauvegardées dans un fichier local simple `replies.json`.

---

## Structure du Projet

```text
/home/nzoy/Scripts/Mailsender/
├── mailsender         # L'exécutable compilé
├── main.go            # Le code source Go (SMTP, IMAP, HTTP Server)
├── recipients.csv     # Liste de vos recruteurs (nom, prénom, e-mail, entreprise)
├── template.html      # Modèle de votre mail d'accompagnement (candidature)
├── dashboard.html     # Modèle HTML/CSS du tableau de bord
├── .env               # Fichier de configuration privé
└── replies.json       # Base de données locale contenant les réponses reçues
```

---

## Configuration

### 1. Fichier de Configuration `.env`

Le fichier `.env` a été pré-créé. Ouvrez-le et configurez vos accès Gmail :

```ini
# Configuration du Serveur SMTP (Envoi)
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587

# Configuration du Serveur IMAP (Réception/Suivi)
IMAP_HOST=imap.gmail.com
IMAP_PORT=993

# Configuration du Serveur Web (Dashboard)
PORT=8080

# Identifiants de Connexion (Gmail nécessite un mot de passe d'application)
SMTP_EMAIL=votre.email@gmail.com
SMTP_PASSWORD=votre_mot_de_passe_d_application
```

> [!IMPORTANT]
> **Configuration de votre compte Gmail** :
> 1. **Mot de passe d'application** : Activez la validation en 2 étapes sur votre compte Google, puis recherchez "Mots de passe d'application". Créez un mot de passe pour "Mailsender" et copiez le code à 16 caractères dans `SMTP_PASSWORD` (sans les espaces).
> 2. **Activer l'IMAP** : Dans les paramètres de votre boîte Gmail en ligne, allez dans l'onglet **Transfert et POP/IMAP** et assurez-vous que **Activer l'IMAP** est coché.

### 2. Destinataires (`recipients.csv`)

Remplissez ce fichier avec les informations des recruteurs :

```csv
email,first_name,last_name,company
recruteur1@entreprise.com,Jean,Dupont,Société Générale
recruteur2@entreprise.com,Marie,Durand,Decathlon
```

*Note : Les colonnes sont automatiquement converties en variables utilisables dans `template.html` (ex: `{{.FirstName}}`, `{{.LastName}}`, `{{.Company}}`).*

---

## Mode 1 : Envoi des E-mails (Cron ou Manuel)

Pour lancer la campagne d'envoi d'e-mails (par exemple pour votre planification automatique) :

```bash
cd /home/nzoy/Scripts/Mailsender
./mailsender
```

Le script enverra les mails aux adresses du CSV un par un, avec le délai configuré dans `.env`.

### Planification Cron (Lundi et Jeudi matin à 08h30)

Pour exécuter automatiquement l'envoi, ajoutez cette tâche planifiée :
1. Lancez : `crontab -e`
2. Ajoutez cette ligne à la fin :
   ```cron
   30 8 * * 1,4 cd /home/nzoy/Scripts/Mailsender && ./mailsender >> mailsender.log 2>&1
   ```

---

## Mode 2 : Tableau de Bord & Suivi des Réponses

Pour démarrer l'interface web de suivi, lancez l'application avec le drapeau `-web` :

```bash
cd /home/nzoy/Scripts/Mailsender
./mailsender -web
```

1. Ouvrez votre navigateur sur : **[http://localhost:8080](http://localhost:8080)**.
2. Cliquez sur le bouton **Synchroniser** dans la barre latérale pour récupérer les e-mails de réponse de vos recruteurs.
3. Les statistiques de réponse se mettent à jour automatiquement.
4. Cliquez sur un candidat dans le tableau pour ouvrir le panneau latéral et lire sa réponse. Vous pouvez également cliquer sur "Répondre par e-mail" pour ouvrir directement votre client mail.
