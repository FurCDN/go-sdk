# go-sdk

[FurCDN](https://www.furcdn.us) 開放 API 的 Go SDK。

完整 API 文檔：<https://docs.furcdn.us/api>

## 安裝

```bash
go get github.com/FurCDN/go-sdk
```

## 使用

```go
package main

import (
	"context"
	"fmt"
	"log"

	furcdn "github.com/FurCDN/go-sdk"
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

	// 選擇性刷快取（只清指定的 URL / 前綴 / glob 樣式）
	sel, _ := client.PurgeCacheWith(ctx, 123, furcdn.PurgeOptions{
		URLs:     []string{"/index.html", "https://example.com/a.css?v=1"},
		Prefixes: []string{"/static/"},
		Patterns: []string{"/assets/*.js"},
	})
	fmt.Printf("purged %d/%d nodes, removed %d entries\n", sel.Success, sel.Total, sel.Removed)

	// 上傳憑證
	_ = client.UploadSSL(ctx, 123, "-----BEGIN CERTIFICATE-----\n...", "-----BEGIN PRIVATE KEY-----\n...")

	// 取得全部節點回源 IP（公開端點，不需 API key）。清單非固定，建議定期重取後同步源站防火牆白名單。
	ips, _ := client.OriginIPs(ctx)
	fmt.Println(ips)
}
```

## 快取清除

`PurgeCache` 與 `PurgeCacheWith` 打的是同一個端點 `POST /api/v1/domains/{id}/purge`，
差別只在請求內容：不帶 body 就是清除整個域名。

```go
// 1. 清除整個域名（舊行為，等同不帶 body）
resp, err := client.PurgeCache(ctx, 123)
// resp.Mode == "all", resp.Removed == 0

// 2. 精確清除指定 URL
resp, err = client.PurgeCacheWith(ctx, 123, furcdn.PurgeOptions{
	URLs: []string{"/b.js", "https://example.com/a.css?v=1"},
})
// resp.Mode == "selective", resp.Removed > 0

// 3. 路徑前綴清除（query 忽略）
resp, err = client.PurgeCacheWith(ctx, 123, furcdn.PurgeOptions{
	Prefixes: []string{"/static/"}, // 同時清掉 /static/a.js、/static/x/y.css
})

// 4. glob 樣式清除（* 可匹配任意字元，含 "/"）
resp, err = client.PurgeCacheWith(ctx, 123, furcdn.PurgeOptions{
	Patterns: []string{"/assets/*.js", "/img/*"},
})

// 5. 混合使用（三者取聯集）
resp, err = client.PurgeCacheWith(ctx, 123, furcdn.PurgeOptions{
	URLs:     []string{"/index.html"},
	Prefixes: []string{"/static/"},
	Patterns: []string{"/assets/*.js"},
})
```

`PurgeOptions` 各欄位語意：

| 欄位 | 說明 |
| --- | --- |
| `All` | 清除整個域名。不可與其他欄位同時使用（伺服器會回 400）。 |
| `URLs` | 精確 URL 清除。可為絕對 URL（host 必須等於該域名）或以 `/` 開頭的路徑。query string 屬於比對條件，例如 `/a.css` 不會清掉 `/a.css?v=1`；除非域名開啟了 ignoreQueryString。會一併清掉該 URL 的所有快取變體（Vary 副本、GET 與 HEAD）。 |
| `Prefixes` | 以 URL 路徑做字串前綴比對（query 忽略）。也可傳絕對 URL，只取其路徑部分。 |
| `Patterns` | 以 URL 路徑做 glob 比對，`*` 可匹配任意字元（含 `/`），必須以 `/` 開頭。 |

限制與回應：

- `urls + prefixes + patterns` 合計最多 100 筆，單筆最多 2048 字元，且不可為空字串。
- 這些條件會在 SDK 端先驗證，不合法時直接回傳可被 `errors.Is(err, furcdn.ErrInvalidPurgeOptions)` 判定的錯誤，**不會送出請求**。
- `total` 是正在服務該域名的節點數，`success` 是已確認的節點數；`removed` 只在選擇性清除時有值，代表各節點刪除的快取項目總數（同一 URL 快取在 3 個節點就算 3 筆）。
- 速率限制：整域清除每人每 5 分鐘 10 次，選擇性清除每人每 5 分鐘 60 次，超額回 429。

```go
_, err := client.PurgeCacheWith(ctx, 123, furcdn.PurgeOptions{})
if errors.Is(err, furcdn.ErrInvalidPurgeOptions) {
	fmt.Println("參數不合法，未送出請求：", err)
}
```

> 快取規則（TTL／「不快取」規則）只在節點回源時套用，修改規則不會動到已經快取的內容。
> 想讓規則變更立即生效，請自行清除對應的快取。

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
