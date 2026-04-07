# ollama-proxy

A locally running Ollama-compatible proxy service supporting seamless integration with multiple model providers

## Features

- 🦙 Partial Ollama API compatibility: Implements core APIs for VSCode Copilot integration
- 🛡️ Local proxy service: Provides Ollama-like API interface at 127.0.0.1:11434
- 🔌 Multi-provider support: Integration with Novita/SiliconFlow/Groq/xAI and other major platforms
- 🔄 Dynamic configuration: Reuses Continue.dev configuration standard with YAML hot reload
- 🧩 Protocol adaptation: Full implementation of Ollama core API specifications
- 🔍 Debug mode: Detailed request/response logging

## Use Cases

- ✅ VSCode Copilot custom model integration (verified compatibility)

## Implemented APIs

| Endpoint                | Method | Description                  | Compatibility |
|-------------------------|--------|------------------------------|---------------|
| `/api/version`          | GET    | Get Ollama version info      | ✅ 100%       |
| `/v1/models`            | GET    | Get available model list     | ✅ 100%       |
| `/api/tags`             | GET    | Get model tag information    | ✅ 100%       |
| `/api/show`             | POST   | View model details           | ✅ 100%       |
| `/v1/chat/completions`  | POST   | Chat completion (proxy)      | ✅ 100%       |

## Configuration Details

This proxy reuses the [Continue.dev](https://docs.continue.dev/reference/) configuration standard and can directly use existing Continue configurations:

```yaml
ollamaVersion: 0.18.2
listenAddress: "127.0.0.1:11434"  # Optional, defaults to "127.0.0.1:11434", set to "0.0.0.0:11434" when using a container
models:
  - name: Novita deepseek v3
    provider: novita
    model: deepseek/deepseek-v3-0324
    apiKey: sk_xxxxx
    capabilites:
      - completion
      - thinking
  - name: Inference.net DeepSeek V3
    provider: openai
    apiBase: https://api.inference.net/v1
    model: deepseek/deepseek-v3-0324/fp-8
    apiKey: inference-xxxxx
  - name: Siliconflow DeepSeek-V3
    provider: siliconflow
    model: deepseek-ai/DeepSeek-V3
    apiKey: sk-xxxxxx
```

## Development Guide

Build command:
```bash
go build -o ollama-proxy
```

Start service:
```bash
./ollama-proxy -config /path/to/config.yaml
```

### Container Deployment

Build image:

```bash
docker build -t ollama-proxy:latest .
```

Run container:

```bash
docker run -d \
  --name ollama-proxy \
  -p 11434:11434 \
  -v /path/to/your/config.yaml:/data/config.yaml \
  ollama-proxy:latest
```

## FAQ

### How to enable debug mode?
Add `-debug` parameter when starting:
```bash
./ollama-proxy -debug
```

### How to apply configuration changes?
The service automatically monitors config file changes and reloads immediately

### How to verify if proxy is working?
```bash
curl http://127.0.0.1:11434/v1/models
```

### How are multi-provider requests routed?
Automatically matched by `model` field in request to `name` in configuration

## Contributing

We welcome contributions from the community!
