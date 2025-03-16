FROM golang:1.24

# Installer Node.js et npm
RUN curl -fsSL https://deb.nodesource.com/setup_18.x | bash - && \
    apt-get update && apt-get install -y nodejs

# Installer Chromium et quelques polices
RUN apt-get update && apt-get install -y \
    chromium \
    fonts-ipafont-gothic fonts-wqy-zenhei fonts-thai-tlwg fonts-kacst \
    && rm -rf /var/lib/apt/lists/*

# Définir les variables d'environnement pour Chromium
ENV PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=true
ENV PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium
ENV CHROME_NO_SANDBOX=1

# Définir le répertoire de travail
WORKDIR /app

# Copier les dépendances et télécharger les modules Go
COPY go.mod go.sum ./
RUN go mod download

# Copier le code source
COPY . .

# Installer Marp CLI
RUN npm install @marp-team/marp-cli

# Compiler l'application
RUN go build -o main .

# Exposer le port
EXPOSE 8080

# Commande de lancement de l'application
CMD ["./main"]