# 运行时只用 alpine:3.20（本地通常已缓存），避免拉取 docker.io 的 node/nginx。
# 前端产物在镜像构建前由 Makefile 在宿主机执行 npm run build 生成。

FROM alpine:3.20

RUN apk add --no-cache nginx wget \
	&& mkdir -p /run/nginx /usr/share/nginx/html

COPY nginx.main.conf /etc/nginx/nginx.conf
COPY nginx.conf /etc/nginx/http.d/default.conf
COPY dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]

HEALTHCHECK --interval=5s --timeout=3s --retries=20 \
	CMD wget -qO- http://127.0.0.1/web-healthz || exit 1
