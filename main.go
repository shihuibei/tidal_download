package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

//go:embed templates/index.html
var indexHTML embed.FS

const (
	baseURL = "https://tidal.401658.xyz"
	timeout = 10 * time.Second
)

type Response struct {
	Items []interface{} `json:"items,omitempty"`
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "bsd", etc.
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

func main() {
	r := gin.Default()

	// 配置 CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// 主页路由
	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html")
		html, err := indexHTML.ReadFile("templates/index.html")
		if err != nil {
			fmt.Printf("[ERROR] 读取 index.html 失败: %v\n", err)
			c.String(http.StatusInternalServerError, "服务器错误")
			return
		}
		c.String(http.StatusOK, string(html))
		fmt.Println("[INFO] 成功加载主页")
	})

	// 搜索 API
	r.GET("/api/search/", func(c *gin.Context) {
		s := c.Query("s")
		if s == "" {
			fmt.Println("[WARN] 搜索关键词为空")
			c.JSON(http.StatusBadRequest, gin.H{"error": "搜索关键词不能为空"})
			return
		}
		fmt.Printf("[INFO] 收到搜索请求，关键词: %s\n", s)

		// 对搜索关键词进行 URL 编码
		encoded := url.QueryEscape(s)
		resp, err := httpGet(fmt.Sprintf("%s/search/?s=%s", baseURL, encoded))
		if err != nil {
			fmt.Printf("[ERROR] 搜索请求失败: %v\n", err)
			c.JSON(http.StatusOK, gin.H{"items": []interface{}{}})
			return
		}
		fmt.Printf("[INFO] 搜索请求成功，响应大小: %d 字节\n", len(resp))

		// 解析响应 JSON
		var data map[string]interface{}
		if err := json.Unmarshal(resp, &data); err != nil {
			fmt.Printf("[ERROR] 解析搜索响应 JSON 失败: %v\n", err)
			c.JSON(http.StatusOK, gin.H{"items": []interface{}{}})
			return
		}

		// 如果有 items 字段，只处理前三条数据
		if items, ok := data["items"].([]interface{}); ok && len(items) > 0 {
			type coverResult struct {
				index int
				resp []byte
				err  error
			}

			// 限制处理数量为3
			maxItems := 3
			if len(items) > maxItems {
				items = items[:maxItems]
				data["items"] = items
				fmt.Printf("[INFO] 截取前三条搜索结果，原始结果数: %d\n", len(items))
			}

			// 创建channel用于接收封面请求的结果
			resultChan := make(chan coverResult, len(items))

			// 并行请求每个item的封面
			for i, item := range items {
				go func(index int, item interface{}) {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if id, ok := itemMap["id"].(float64); ok {
							resp, err := httpGet(fmt.Sprintf("%s/cover/?id=%d", baseURL, int(id)))
							resultChan <- coverResult{index: index, resp: resp, err: err}
							return
						}
					}
					resultChan <- coverResult{index: index, err: fmt.Errorf("无效的item数据")}
				}(i, item)
			}

			// 收集所有封面请求的结果
			results := make([]coverResult, len(items))
			for i := 0; i < len(items); i++ {
				result := <-resultChan
				results[result.index] = result
			}

			// 处理结果
			for i, result := range results {
				if result.err != nil {
					fmt.Printf("[ERROR] 获取封面失败 (index %d): %v\n", i, result.err)
					if itemMap, ok := items[i].(map[string]interface{}); ok {
						itemMap["coverUrl"] = "https://via.placeholder.com/640"
					}
					continue
				}

				var coverData []map[string]interface{}
				if err := json.Unmarshal(result.resp, &coverData); err != nil {
					fmt.Printf("[ERROR] 解析封面数据失败 (index %d): %v\n", i, err)
					if itemMap, ok := items[i].(map[string]interface{}); ok {
						itemMap["coverUrl"] = "https://via.placeholder.com/640"
					}
					continue
				}

				if len(coverData) > 0 && coverData[0]["640"] != nil {
					if itemMap, ok := items[i].(map[string]interface{}); ok {
						itemMap["coverUrl"] = coverData[0]["640"]
					}
				}
			}
		}

		// 返回处理后的数据
		c.JSON(http.StatusOK, data)
	})

	// 封面 API
	r.GET("/api/cover/", func(c *gin.Context) {
		id := c.Query("id")
		if id == "" {
			fmt.Println("[WARN] 封面 ID 为空")
			c.JSON(http.StatusBadRequest, gin.H{"error": "ID不能为空"})
			return
		}
		fmt.Printf("[INFO] 请求封面，ID: %s\n", id)

		resp, err := httpGet(fmt.Sprintf("%s/cover/?id=%s", baseURL, id))
		if err != nil {
			fmt.Printf("[ERROR] 获取封面失败: %v\n", err)
			c.JSON(http.StatusOK, []map[string]string{{
				"80":   "https://via.placeholder.com/80",
				"640":  "https://via.placeholder.com/640",
				"1280": "https://via.placeholder.com/1280",
			}})
			return
		}
		fmt.Printf("[INFO] 成功获取封面，响应大小: %d 字节\n", len(resp))

		c.Data(http.StatusOK, "application/json", resp)
	})

	// 音轨 API
	r.GET("/api/track/", func(c *gin.Context) {
		id := c.Query("id")
		quality := c.Query("quality")
		if id == "" || quality == "" {
			fmt.Println("[WARN] 音轨 ID 或 quality 为空")
			c.JSON(http.StatusBadRequest, gin.H{"error": "ID和quality不能为空"})
			return
		}
		fmt.Printf("[INFO] 请求音轨，ID: %s, Quality: %s\n", id, quality)

		resp, err := httpGet(fmt.Sprintf("%s/track/?id=%s&quality=%s", baseURL, id, quality))
		if err != nil {
			fmt.Printf("[ERROR] 获取音轨失败: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取音轨失败"})
			return
		}
		fmt.Printf("[INFO] 成功获取音轨，响应大小: %d 字节\n", len(resp))

		// 解析响应 JSON 为数组
		var items []map[string]interface{}
		if err := json.Unmarshal(resp, &items); err != nil {
			fmt.Printf("[ERROR] 解析音轨响应 JSON 失败: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "解析音轨数据失败"})
			return
		}

		// 遍历数组找到包含 OriginalTrackUrl 的对象
		for _, item := range items {
			if url, ok := item["OriginalTrackUrl"].(string); ok {
				c.JSON(http.StatusOK, gin.H{"originalTrackUrl": url})
				return
			}
		}

		c.JSON(http.StatusNotFound, gin.H{"error": "未找到音轨 URL"})
	})

	// 艺术家 API
	r.GET("/api/artist/", func(c *gin.Context) {
		s := c.Query("s")
		if s == "" {
			fmt.Println("[WARN] 艺术家搜索关键词为空")
			c.JSON(http.StatusBadRequest, gin.H{"error": "搜索关键词不能为空"})
			return
		}
		fmt.Printf("[INFO] 搜索艺术家，关键词: %s\n", s)

		resp, err := httpGet(fmt.Sprintf("%s/artist/?s=%s", baseURL, s))
		if err != nil {
			fmt.Printf("[ERROR] 搜索艺术家失败: %v\n", err)
			c.JSON(http.StatusOK, Response{Items: []interface{}{}})
			return
		}
		fmt.Printf("[INFO] 成功搜索艺术家，响应大小: %d 字节\n", len(resp))

		c.Data(http.StatusOK, "application/json", resp)
	})

	// 艺术家音轨 API
	r.GET("/api/artist/tracks/", func(c *gin.Context) {
		id := c.Query("id")
		if id == "" {
			fmt.Println("[WARN] 艺术家 ID 为空")
			c.JSON(http.StatusBadRequest, gin.H{"error": "ID不能为空"})
			return
		}
		fmt.Printf("[INFO] 请求艺术家音轨，ID: %s\n", id)

		resp, err := httpGet(fmt.Sprintf("%s/artist/tracks/?id=%s", baseURL, id))
		if err != nil {
			fmt.Printf("[ERROR] 获取艺术家音轨失败: %v\n", err)
			c.JSON(http.StatusOK, Response{Items: []interface{}{}})
			return
		}
		fmt.Printf("[INFO] 成功获取艺术家音轨，响应大小: %d 字节\n", len(resp))

		c.Data(http.StatusOK, "application/json", resp)
	})

	// 启动服务器并自动打开浏览器
	go func() {
		time.Sleep(500 * time.Millisecond) // 等待服务器启动
		url := "http://localhost:9527"
		if err := openBrowser(url); err != nil {
			fmt.Printf("[ERROR] 无法打开浏览器: %v\n", err)
		}
	}()



	fmt.Println("[INFO] 服务器启动在 :9527 端口")
	r.Run(":9527")
}

func httpGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// 添加 User-Agent 头部
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}