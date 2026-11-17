package panel

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPISpec []byte

func openAPIDocument(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(openAPISpec)
}

const scalarHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>趣多多流量冠军 · 接口文档</title>
</head>
<body>
  <div id="reference"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.78.1"></script>
  <script>Scalar.createApiReference('#reference', { url: '/openapi.yaml', theme: 'kepler' })</script>
</body>
</html>`

func scalarPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(scalarHTML))
}
