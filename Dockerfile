# Étape de build
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copier les fichiers go.mod et go.sum
COPY go.mod go.sum ./

# Télécharger les dépendances
RUN go mod download

# Copier le code source
COPY . .

# Compiler l'application (statique)
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Étape finale
FROM alpine:3.19

WORKDIR /app

# Installer les dépendances d'exécution nécessaires
RUN apk update && apk add --no-cache \
    ca-certificates \
    nodejs \
    npm \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ttf-freefont \
    font-noto-emoji \
    && mkdir -p /tmp/cmu-fonts /usr/share/fonts/truetype/cmu \
    && wget -q -O /tmp/cm-unicode.tar.xz "https://sourceforge.net/projects/cm-unicode/files/cm-unicode/0.7.0/cm-unicode-0.7.0-ttf.tar.xz/download" \
    && tar -xf /tmp/cm-unicode.tar.xz -C /tmp/cmu-fonts \
    && cp /tmp/cmu-fonts/cm-unicode-0.7.0/*.ttf /usr/share/fonts/truetype/cmu/ \
    && fc-cache -f \
    && rm -rf /tmp/cmu-fonts /tmp/cm-unicode.tar.xz

# Configurer les variables d'environnement pour Chromium et Puppeteer
ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true
ENV PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium-browser
ENV CHROME_DISABLE_GPU=1
ENV CHROME_NO_SANDBOX=1

# Installer Marp CLI globalement
RUN npm install -g @marp-team/marp-cli

# Copier l'exécutable depuis l'étape de build
COPY --from=builder /app/main .

# Exposer le port de l'application
EXPOSE 8080

# Lancer l'application
CMD ["./main"]
