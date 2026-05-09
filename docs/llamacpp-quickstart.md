# OpenAI-Compatible API Quickstart (llama.cpp example)

agentd works with any OpenAI-compatible API endpoint. This guide shows how to set up llama.cpp as a local LLM provider, but you can substitute any compatible service (Ollama, LM Studio, vLLM, etc.).

## Quick Setup with Docker

### 1. Create Project Structure

```bash
mkdir -p ~/llamacpp-agentd/{models,agentd}
cd ~/llamacpp-agentd
```

### 2. Download a GGUF Model

```bash
# Example: Download TinyLlama (small, fast for testing)
wget -O models/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf \
  https://huggingface.co/TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf

# Or use any GGUF model from Hugging Face
```

### 3. Create docker-compose.yml

```yaml
version: "3.8"
services:
  llamacpp:
    image: ghcr.io/ggml-org/llama.cpp:full
    ports:
      - "8080:8080"
    volumes:
      - ./models:/models
    command: |
      -m /models/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf
      --host 0.0.0.0
      --port 8080
      --ctx-size 4096
```

### 4. Start the Server

```bash
docker-compose up -d
# Wait for the model to load (check logs: docker-compose logs -f)
```

### 5. Test the Endpoint

```bash
curl http://localhost:8080/v1/chat/completions \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "model": "tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Direct Installation (No Docker)

If you prefer to run llama.cpp directly without Docker:

### Linux/macOS

```bash
# Build from source
git clone https://github.com/ggml-org/llama.cpp
cd llama.cpp
make

# Run server
./server -m /path/to/your/model.gguf -p 8080 --host 0.0.0.0
```

### Using Pre-built Binaries

```bash
# Download from releases
curl -L https://github.com/ggml-org/llama.cpp/releases/latest/download/llama-server-linux-x64 -o llama-server
chmod +x llama-server

# Run
./llama-server -m models/your-model.gguf -p 8080
```

## Configure agentd

Create `~/.agentd/config.yaml`:

```yaml
gateway:
  order: [llamacpp]
  llamacpp:
    base_url: "http://127.0.0.1:8080"
    model: "tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"  # Must match your GGUF filename
    timeout: "10m"  # Local inference can be slow

api:
  address: "127.0.0.1:8765"
```

### Initialize agentd

```bash
# If not already initialized
agentd init

# Verify config loads
agentd status
```

## Test with agentd

### Via cURL (direct to agentd API)

```bash
curl http://127.0.0.1:8765/v1/chat/completions \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "Create a todo list web application in React using REST API"}
    ]
  }'
```

### Via CLI

```bash
# Ask command sends config from your YAML
agentd ask "Create a todo list web application in React using REST API"
```

## Using Other OpenAI-Compatible Providers

Simply change the config in `~/.agentd/config.yaml`:

### Ollama
```yaml
gateway:
  order: [ollama]
  ollama:
    base_url: "http://127.0.0.1:11434"
    model: "llama3:8b"
```

### OpenAI (API key required)
```yaml
gateway:
  order: [openai]
  openai:
    base_url: "https://api.openai.com/v1"
    model: "gpt-4o-mini"
    api_key: "sk-your-key-here"
```

### LM Studio
```yaml
gateway:
  order: [openai]  # Uses OpenAI-compatible provider
  openai:
    base_url: "http://127.0.0.1:1234"
    model: "local-model"
    api_key: "not-required"
```

### vLLM
```yaml
gateway:
  order: [openai]
  openai:
    base_url: "http://127.0.0.1:8000/v1"
    model: "your-model-name"
```

## Troubleshooting

### "No LLM providers available"

1. Check the endpoint is running: `curl http://localhost:8080/v1/models`
2. Verify the model filename in config matches the actual GGUF file
3. Ensure `gateway.order` includes your provider name

### Slow responses

- Local inference is inherently slower than cloud APIs
- Consider using a smaller quantized model (Q4_K_M or Q5_K_M)
- Increase `timeout` in config if needed

### "connection refused"

- Make sure the llama.cpp server is running: `docker-compose ps`
- Check the port is not in use: `lsof -i :8080`

### Out of memory

- Use a smaller model
- Reduce context size: `--ctx-size 2048`
- Use quantization (Q4_K_M instead of Q8_0)