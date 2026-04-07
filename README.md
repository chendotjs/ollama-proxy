# ollama-proxy
[English Version](README_EN.md) | [中文版](README.md)

本地运行的Ollama兼容代理服务，支持多模型供应商无缝集成

## 功能介绍

- 🦙 Ollama 部分接口兼容：实现核心API用于VSCode Copilot集成
- 🛡️ 本地代理服务：在127.0.0.1:11434提供类Ollama API接口
- 🔌 多供应商支持：Novita/SiliconFlow/Groq/xAI等主流平台接入
- 🔄 动态配置：复用Continue.dev配置规范，支持YAML热重载
- 🧩 协议适配：完整实现Ollama核心API接口规范
- 🔍 调试模式：详细请求/响应日志追踪

## 使用场景

- ✅ VSCode Copilot自定义模型接入（已验证兼容性）

## 已实现接口

| 端点                 | 方法 | 功能描述                     | 兼容性 |
|----------------------|------|----------------------------|--------|
| `/api/version`       | GET  | 获取Ollama版本信息           | ✅ 100% |
| `/v1/models`         | GET  | 获取可用模型列表             | ✅ 100% |
| `/api/tags`          | GET  | 获取模型标签信息             | ✅ 100% |
| `/api/show`          | POST | 查看模型详细信息             | ✅ 100% |
| `/v1/chat/completions` | POST | 聊天补全接口（代理转发）     | ✅ 100% |

## 配置详解

本代理复用 [Continue.dev](https://docs.continue.dev/reference/) 的配置规范，可直接使用现有Continue配置：

```yaml
ollamaVersion: 0.18.2
listenAddress: "127.0.0.1:11434"  # 可选，默认为 "127.0.0.1:11434"，若需要在容器中运行，这里设置为0.0.0.0:11434
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

## 开发指南

构建命令：
```bash
go build -o ollama-proxy
```

启动服务：
```bash
./ollama-proxy -config /path/to/config.yaml
```

### 容器化部署

构建镜像：

```bash
docker build -t ollama-proxy:latest .
```

运行容器：

```bash
docker run -d \
  --name ollama-proxy \
  -p 11434:11434 \
  -v /path/to/your/config.yaml:/data/config.yaml \
  ollama-proxy:latest
```

## 常见问题

### 如何启用调试模式？
启动时添加 `-debug` 参数：
```bash
./ollama-proxy -debug
```

### 配置修改后如何生效？
服务会自动监控配置文件变更，保存后立即生效

### 如何验证代理是否正常工作？
```bash
curl http://127.0.0.1:11434/v1/models
```

### 多供应商请求如何路由？
根据请求中的 `model` 字段自动匹配配置中的 `name` 项

## 贡献指南

欢迎社区贡献！


