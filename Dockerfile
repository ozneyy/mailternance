# ─────────────────────────────────────────────
#  Stage 1 : Build
# ─────────────────────────────────────────────
FROM golang:1.21 AS builder

WORKDIR /build

# Copier uniquement go.mod / go.sum d'abord (cache des dépendances)
COPY go.mod go.sum ./
RUN go mod download

# Copier le reste du code source
COPY main.go ./
COPY backend/ ./backend/
COPY web/ ./web/

# Compiler un binaire statique pour Linux (pas de CGO)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o mailternance main.go

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
COPY --from=builder /build/mailternance .

# Copier les assets statiques
COPY web/ ./web/

# Créer les dossiers de données persistants
RUN mkdir -p web/attachments logs data

# Le .env et recipients.csv sont fournis via volume ou variables d'env
# Port exposé (correspondra à PORT dans .env)
EXPOSE 17890

# Démarrer en mode web (dashboard)
ENTRYPOINT ["./mailternance", "-web"]
