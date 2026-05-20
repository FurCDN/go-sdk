# FurCDN-API-Go

[FurCDN](https://www.furcdn.us) 開放 API 的 Go SDK。

完整 API 文檔：<https://docs.furcdn.us/api>

## 安裝

```bash
go get github.com/FurCDN/FurCDN-API-Go
```

## 使用

```go
package main

import (
	"context"
	"fmt"
	"log"

	furcdn "github.com/FurCDN/FurCDN-API-Go"
)

func main() {
	client := furcdn.New("fck_xxxxxxxx_xxxxxxxxxxxxxxxxxxxxxxxx")
	ctx := context.Background()

	// 列出域名
	domains, err := client.ListDomains(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range domains {
		fmt.Printf("%d  %s  enabled=%v\n", d.ID, d.Name, d.Enabled)
	}

	// 刷快取
	resp, _ := client.PurgeCache(ctx, 123)
	fmt.Printf("purged %d/%d nodes\n", resp.Success, resp.Total)

	// 上傳憑證
	_ = client.UploadSSL(ctx, 123, "-----BEGIN CERTIFICATE-----\n...", "-----BEGIN PRIVATE KEY-----\n...")
}
```

## 錯誤處理

非 2xx 回應會回傳 `*furcdn.APIError`：

```go
domains, err := client.ListDomains(ctx)
if err != nil {
	if apiErr, ok := err.(*furcdn.APIError); ok {
		fmt.Printf("HTTP %d: %s\n", apiErr.StatusCode, apiErr.Message)
	}
}
```

## 開發

```bash
go test ./...
```

## License

MIT
