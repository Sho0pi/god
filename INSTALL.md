# god — setup guide

## Prerequisites

### 1. Go 1.22+
```sh
brew install go
```

### 2. Gemini API key
Get one free at https://aistudio.google.com/  
```sh
export GEMINI_API_KEY=<your-key>
```
Add to your `.env` file or shell profile.

### 3. ddg-search (web search tool)
```sh
go install github.com/Djarvur/ddg-search/cmd/ddg-search@latest
```
No API key needed. Uses DuckDuckGo HTML search.

### 4. Docker + PostgreSQL (long-term memory)
```sh
brew install --cask docker
docker-compose up -d
```
Sets up pgvector at `localhost:5432`. Credentials in `docker-compose.yml`.

Set `DATABASE_URL` in your `.env`:
```
DATABASE_URL=postgres://god:god@localhost:5432/god
```

### 5. Google Places API key (optional — places tool)
```sh
export GOOGLE_PLACES_API_KEY=<your-key>
```

## Config

Copy the example config:
```sh
cp god.example.yaml god.yaml
```

Edit `god.yaml` — set your phone number in `connectors.whatsapp.allow`.

## Run

```sh
# Check all deps are ready
go run . doctor

# Chat in terminal
go run . cli

# Connect WhatsApp (scan QR on first run)
go run . whatsapp
```

## Build binary

```sh
go build -o god .
./god doctor
./god whatsapp
```
