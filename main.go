package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// OllamaTag represents a single model tag in the /api/tags response
type OllamaTag struct {
	Name       string    `json:"name"`
	Model      string    `json:"model"` // Often the same as name for unique models
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`   // Proxy doesn't know the real size, use 0
	Digest     string    `json:"digest"` // Proxy doesn't have a digest, leave empty or use a placeholder
	Details    struct {
		Format            string   `json:"format"`
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	} `json:"details"`
}

// OllamaTagsResponse represents the response for the /api/tags endpoint
type OllamaTagsResponse struct {
	Models []OllamaTag `json:"models"`
}

type Config struct {
	OllamaVersion string           `yaml:"ollamaVersion,omitempty"`
	ListenAddress string           `yaml:"listenAddress,omitempty"`
	Models        []ProviderConfig `yaml:"models"`
}

type responseRecorder struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

var providerAPIBaseMap = map[string]string{
	"novita":      "https://api.novita.ai/v3/openai",
	"siliconflow": "https://api.siliconflow.cn/v1",
	"groq":        "https://api.siliconflow.cn/v1",
	"xAI":         "https://api.x.ai/v1",
	"gemini":      "https://generativelanguage.googleapis.com/v1beta/openai",
}

type ProviderConfig struct {
	Name          string   `yaml:"name"`
	Provider      string   `yaml:"provider"`
	APIBase       string   `yaml:"apiBase,omitempty"`
	Model         string   `yaml:"model"`
	APIKey        string   `yaml:"apiKey"`
	SystemMessage string   `yaml:"systemMessage"`
	Modelfile     string   `yaml:"modelfile,omitempty"`
	Parameters    string   `yaml:"parameters,omitempty"`
	Template      string   `yaml:"template,omitempty"`
	Capabilities  []string `yaml:"capabilities,omitempty"`
}

var (
	config     Config
	configPath string
	debugFlag  bool
	configLock sync.RWMutex
)

func main() {
	flag.StringVar(&configPath, "config", "~/.continue/config.yaml", "path to config file")
	flag.BoolVar(&debugFlag, "debug", false, "enable debug logging")
	flag.Parse()

	loadConfig()
	startWatcher()

	// 关闭 gin 的调试日志，除非明确启用 debugFlag
	if !debugFlag {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	r := gin.Default()

	// 添加日志中间件（如果需要）
	if debugFlag {
		r.Use(gin.Logger())
	}
	r.Use(gin.Recovery())

	r.Any("/v1/chat/*path", proxyHandler)
	r.GET("/v1/models", listModels)
	r.POST("/api/show", showHandler)
	r.GET("/api/tags", tagsHandler)
	r.GET("/api/version", versionHandler)

	// 添加根路径处理程序以进行健康检查或基本信息显示
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "running", "message": "Ollama Proxy is active"})
	})

	go func() {
		configLock.RLock()
		listenAddr := config.ListenAddress
		if listenAddr == "" {
			listenAddr = "127.0.0.1:11434"
		}
		configLock.RUnlock()
		log.Printf("Starting server on http://%s", listenAddr)
		if err := r.Run(listenAddr); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")
}

func loadConfig() {
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	var newConfig Config
	if err := yaml.Unmarshal(data, &newConfig); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	configLock.Lock()
	config = newConfig
	configLock.Unlock()
	log.Println("Config reloaded successfully")
}

func startWatcher() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) {
					loadConfig()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watcher error: %v", err)
			}
		}
	}()

	if err := watcher.Add(configPath); err != nil {
		log.Fatalf("Failed to watch config file: %v", err)
	}
}

func listModels(c *gin.Context) {
	configLock.RLock()
	defer configLock.RUnlock()

	type ModelObject struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	var models []ModelObject

	for _, provider := range config.Models {
		models = append(models, ModelObject{
			ID:      provider.Name,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "ollama-proxy",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

func showHandler(c *gin.Context) {
	var req struct {
		Model string `json:"model"` // 客户端请求使用model字段
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	configLock.RLock()
	defer configLock.RUnlock()

	var target *ProviderConfig
	for i := range config.Models {
		// 根据请求中的model查找匹配的ProviderConfig
		if config.Models[i].Name == req.Model {
			target = &config.Models[i]
			break
		}
	}

	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}

	// 从 ProviderConfig 中获取 Modelfile, Parameters, Template
	modelfileContent := target.Modelfile
	parametersContent := target.Parameters
	templateContent := target.Template

	// 如果配置中没有提供，可以设置默认值或留空
	if modelfileContent == "" {
		modelfileContent = fmt.Sprintf("# Modelfile for %s (proxied)\nFROM scratch", target.Name)
	}
	if parametersContent == "" {
		parametersContent = "# No specific parameters defined in proxy config"
	}
	if templateContent == "" {
		templateContent = `{{ if .System }}System: {{ .System }}{{ end }}
User: {{ .Prompt }}
Assistant: {{ .Response }}`
	}

	// 生成符合 Ollama /api/show 格式的响应
	response := gin.H{
		"license":    "", // 通常为空或需要从上游获取（如果可能）
		"modelfile":  modelfileContent,
		"parameters": parametersContent,
		"template":   templateContent,
		"details": gin.H{ // 提供一些通用的或基于配置的详细信息
			"parent_model":       "",
			"format":             "proxy",
			"family":             "proxy", // 可以尝试从 target.Model 解析，或保持通用
			"families":           nil,
			"parameter_size":     "N/A", // 代理无法确定
			"quantization_level": "N/A", // 代理无法确定
		},
		"model_info": gin.H{
			"general.architecture":                   "llama",
			"general.name":                           target.Name,
			"general.file_type":                      2,
			"general.parameter_count":                0,
			"llama.context_length":                   120000,
			"llama.block_count":                      0,
			"llama.embedding_length":                 0,
			"llama.attention.head_count":             0,
			"llama.attention.head_count_kv":          0,
			"llama.attention.layer_norm_rms_epsilon": 0.00001,
			"llama.feed_forward_length":              0,
			"llama.rope.dimension_count":             0,
			"llama.rope.freq_base":                   500000,
			"llama.vocab_size":                       0,
			"tokenizer.ggml.model":                   "gpt2",
			"tokenizer.ggml.bos_token_id":            0,
			"tokenizer.ggml.eos_token_id":            0,
		},
		"capabilities": target.Capabilities,
	}

	c.JSON(http.StatusOK, response)
}

func proxyHandler(c *gin.Context) {
	// ... (读取请求体并解析 model name 的逻辑保持不变) ...
	var requestBodyBytes []byte
	if c.Request.Body != nil {
		requestBodyBytes, _ = io.ReadAll(c.Request.Body)
		// 重新填充请求体，因为后续需要再次读取
		c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBodyBytes))
	}

	var openaiReq struct {
		Model string `json:"model"` // 客户端请求中的模型名称 (对应配置中的 name)
		// ... 其他可能的 OpenAI 请求字段
	}
	// 使用原始请求体进行解析
	if err := json.Unmarshal(requestBodyBytes, &openaiReq); err != nil {
		// 如果解析失败，可能不是 JSON 请求或格式不符，但仍可能需要代理（例如流式请求）
		// 暂时记录错误，但继续尝试代理，让后端处理错误
		log.Printf("[WARN] Failed to parse incoming request body for model extraction: %v", err)
		// 对于无法解析 model 的情况，需要决定如何处理。
		// 选项1：返回错误
		// c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body, cannot determine model"})
		// return
		// 选项2：尝试使用默认模型或第一个模型（如果适用）
		// 选项3：继续代理，让后端处理（当前选择）
	}

	configLock.RLock()
	var target *ProviderConfig
	// 根据客户端请求的 model (即配置中的 name) 查找目标配置
	if openaiReq.Model != "" {
		for i := range config.Models {
			if config.Models[i].Name == openaiReq.Model {
				target = &config.Models[i]
				break
			}
		}
	}
	configLock.RUnlock() // 尽早释放读锁

	if target == nil {
		// 如果请求体中没有 model 或找不到匹配的模型
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("model '%s' not found in proxy configuration", openaiReq.Model)})
		return
	}

	// ... (创建 Reverse Proxy 的逻辑保持不变) ...
	apiBase := target.APIBase
	if apiBase == "" {
		if base, ok := providerAPIBaseMap[target.Provider]; ok {
			apiBase = base
		} else {
			log.Printf("[ERROR] No APIBase provided and no mapping for provider: %s", target.Provider)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid upstream configuration - missing API base URL"})
			return
		}
	}

	targetURL, err := url.Parse(apiBase)
	if err != nil {
		log.Printf("[ERROR] Invalid APIBase '%s' for model %s: %v", apiBase, target.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error - invalid upstream configuration"})
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Configure transport to use environment proxy settings
	// This will automatically use HTTP_PROXY, HTTPS_PROXY, and NO_PROXY
	proxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		// Copy other potentially important defaults from http.DefaultTransport
		// to maintain similar connection behavior (timeouts, keep-alives, etc.)
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// 记录原始请求 (如果 debug 开启)
	if debugFlag {
		// 确保 requestBodyBytes 在这里仍然可用
		log.Printf("[DEBUG] Incoming Request:\nMethod: %s\nURL: %s\nHeaders: %v\nBody: %s\n",
			c.Request.Method,
			c.Request.URL.String(),
			c.Request.Header,
			string(requestBodyBytes)) // 使用之前读取的 body
	}
	// 恢复请求体，以便 Director 函数可以读取
	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBodyBytes))

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req) // 应用默认的 Director 逻辑 (设置 Host, Scheme 等)

		// 重写 URL 路径
		// 例如：
		// APIBase https://api.inference.net/v1
		// 目标 URL 应该是 https://api.inference.net/v1/chat/completions
		// APIBase https://api.novita.ai/v3/openai
		// 目标 URL 应该是 https://api.novita.ai/v3/openai/chat/completions
		basePath := strings.TrimSuffix(targetURL.Path, "/")
		req.URL.Path = basePath + "/chat/completions"

		// 设置认证和其他必要的头信息
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", target.APIKey))
		// 可能需要移除或修改 Host 头，httputil 通常会处理好
		req.Host = targetURL.Host

		// 修改请求体：将 model 字段替换为目标服务商的模型 ID
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			log.Printf("[ERROR] Failed to read request body in director: %v", err)
			// 可能需要返回错误或允许请求继续（如果 body 不是必需的）
			return
		}
		req.Body.Close() // 关闭原始 body

		var bodyMap map[string]interface{}
		var newBody []byte
		// 只有在 body 是有效的 JSON 时才尝试修改
		if json.Unmarshal(bodyBytes, &bodyMap) == nil {
			originalModel, _ := bodyMap["model"].(string) // 记录原始模型名称以供日志记录
			bodyMap["model"] = target.Model               // 替换为目标模型 ID
			// 可以在这里注入 system message (如果需要且请求格式支持)
			// 例如，如果请求中有 messages 数组
			/*
				if messages, ok := bodyMap["messages"].([]interface{}); ok && target.SystemMessage != "" {
					// 检查是否已存在 system message
					hasSystem := false
					for _, msg := range messages {
						if m, ok := msg.(map[string]interface{}); ok && m["role"] == "system" {
							hasSystem = true
							break
						}
					}
					// 如果没有 system message，则添加一个
					if !hasSystem {
						systemMsg := map[string]interface{}{"role": "system", "content": target.SystemMessage}
						bodyMap["messages"] = append([]interface{}{systemMsg}, messages...)
					}
				}
			*/

			newBody, err = json.Marshal(bodyMap)
			if err != nil {
				log.Printf("[ERROR] Failed to marshal modified request body: %v", err)
				// 如果序列化失败，恢复原始 body
				newBody = bodyBytes
				// 恢复原始 model 名称，以防日志记录错误
				if bodyMap != nil {
					bodyMap["model"] = originalModel
				}
			}
		} else {
			// 如果 body 不是 JSON 或解析失败，按原样转发
			newBody = bodyBytes
		}

		req.Body = io.NopCloser(bytes.NewBuffer(newBody))
		req.ContentLength = int64(len(newBody))
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
		// 确保 Content-Type 正确设置，通常是 application/json
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}

		// 记录修改后的请求 (如果 debug 开启)
		if debugFlag {
			log.Printf("[DEBUG] Outgoing Request:\nMethod: %s\nURL: %s\nHeaders: %v\nBody: %s\n",
				req.Method,
				req.URL.String(),
				req.Header,
				string(newBody)) // 记录修改后的 body
		}
	}

	// 添加错误处理
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		log.Printf("[ERROR] Proxy error: %v", err)
		// 检查错误类型，更优雅地处理连接错误等
		if _, ok := err.(net.Error); ok && err.(net.Error).Timeout() {
			rw.WriteHeader(http.StatusGatewayTimeout)
		} else {
			rw.WriteHeader(http.StatusBadGateway)
		}
		// 可以向客户端返回一个 JSON 错误信息
		json.NewEncoder(rw).Encode(gin.H{"error": "proxy error", "details": err.Error()})
	}

	// 包装响应写入器以记录响应 (如果 debug 开启)
	if debugFlag {
		recorder := &responseRecorder{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		c.Writer = recorder // 替换 gin 的 ResponseWriter

		// 使用 defer 确保在处理程序结束后记录响应
		defer func() {
			// 确保在写入日志前，所有响应头和状态码都已设置
			// gin 的中间件通常能保证这一点
			log.Printf("[DEBUG] Response:\nStatus: %d\nHeaders: %v\nBody: %s\n",
				recorder.Status(),      // 获取最终状态码
				recorder.Header(),      // 获取最终响应头
				recorder.body.String()) // 获取响应体
		}()
	}

	// 执行代理
	proxy.ServeHTTP(c.Writer, c.Request)
}

// tagsHandler handles requests for GET /api/tags
func tagsHandler(c *gin.Context) {
	configLock.RLock()
	defer configLock.RUnlock()

	response := OllamaTagsResponse{
		Models: make([]OllamaTag, 0, len(config.Models)),
	}

	now := time.Now()
	for _, provider := range config.Models {
		tag := OllamaTag{
			Name:       provider.Name,
			Model:      provider.Name, // Use the proxy name as the model identifier
			ModifiedAt: now,           // Use current time as modification time
			Size:       0,             // Size is unknown for proxied models
			Digest:     "",            // Digest is unknown
			Details: struct {
				Format            string   `json:"format"`
				Family            string   `json:"family"`
				Families          []string `json:"families"`
				ParameterSize     string   `json:"parameter_size"`
				QuantizationLevel string   `json:"quantization_level"`
			}{
				Format:            "proxy",
				Family:            "proxy", // Or try to infer from provider.Model if needed
				Families:          nil,
				ParameterSize:     "N/A",
				QuantizationLevel: "N/A",
			},
		}
		response.Models = append(response.Models, tag)
	}

	c.JSON(http.StatusOK, response)
}

func versionHandler(c *gin.Context) {
	configLock.RLock()
	defer configLock.RUnlock()

	version := config.OllamaVersion
	if version == "" {
		version = "unknown"
	}

	c.JSON(http.StatusOK, gin.H{
		"version": version,
	})
}
