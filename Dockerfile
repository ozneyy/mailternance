# ─────────────────────────────────────────────
#  Stage 1 : Build
# ─────────────────────────────────────────────
FROM golang:latest AS builder

WORKDIR /build

# Ignorer la contrainte de version go.mod (go 1.26.4 déclaré mais non publié)
ENV GOTOOLCHAIN=local

# Copier uniquement go.mod / go.sum d'abord (cache des dépendances)
COPY go.mod go.sum ./
RUN go mod download

# Copier le reste du code source
COPY main.go .

# Compiler un binaire statique pour Linux (pas de CGO)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o mailsender .

# ─────────────────────────────────────────────
#  Stage 2 : Image finale minimale
# ─────────────────────────────────────────────
FROM debian:bookworm-slim

# Certificats TLS requis pour SMTP/IMAP Gmail (TLS)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copier le binaire compilé
COPY --from=builder /build/mailsender .

# Copier les assets statiques du dashboard
COPY web/ ./web/

# Copier les templates d'email par défaut
COPY template.html template.txt ./

# Créer les dossiers et fichiers de données persistants
# (ils seront montés en volume en production)
RUN mkdir -p attachments && \
    echo "[]" > sent_history.json && \
    echo "[]" > replies.json && \
    echo '{"subject":"","portfolioUrl":"","links":[]}' > settings.json && \
    echo "[]" > templates.json

# Le .env et recipients.csv sont fournis via volume ou variables d'env
# Port exposé (correspondra à PORT dans .env)
EXPOSE 17890

# Démarrer en mode web (dashboard)
ENTRYPOINT ["./mailsender", "-web"]
