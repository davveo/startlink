# Starlink Web Console

推送运营前端：模板审核、活动创建与进度追踪。通过同源 `/api` 反代到后端。

## 本地开发

先启动后端 API（`:8080`），再：

```bash
cd web
npm install
npm run dev
```

访问 http://localhost:5173 （Vite 已代理 `/api` 与 `/healthz`）。

## Docker

由仓库根目录 `docker compose up -d --build` 一并启动，前端映射 **http://localhost:3000**。
