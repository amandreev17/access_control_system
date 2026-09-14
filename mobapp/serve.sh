#!/bin/bash
#
# SKUD PWA Server — одна команда для запуска всего
# 1) Go-бэкенд          :8080  (API: /api/login, /api/generate-qr, /api/validate-qr)
# 2) HTTPS PWA-сервер   :3443  (build/ + прокси /api/* на Go-бэкенд)
# 3) HTTP-сервер        :3000  (только ca.crt — скачать на телефон)
#
set -u

PROJ_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJ_DIR"

BACKEND_PORT=8080
HTTPS_PORT=3443
HTTP_PORT=3000
CERT_FILE="$PROJ_DIR/certs/server.crt"
KEY_FILE="$PROJ_DIR/certs/server.key"
CA_CRT="$PROJ_DIR/certs/ca.crt"
BUILD_DIR="$PROJ_DIR/build"
LOGS_DIR="$PROJ_DIR/logs"

mkdir -p "$BUILD_DIR" "$PROJ_DIR/certs" "$LOGS_DIR"

echo "==========================================="
echo "  СКУД — HTTPS PWA"
echo "==========================================="

IP=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo "")
[ -n "$IP" ] && echo "🌐 Локальный IP: $IP"

# ---------- Frontend build ----------
# Пересобираем, если сборки нет или исходники новее неё (иначе правки из src/ не попадут в приложение)
NEEDS_BUILD=0
[ ! -f "$BUILD_DIR/index.html" ] && NEEDS_BUILD=1
[ -n "$(find src -type f -newer "$BUILD_DIR/index.html" 2>/dev/null)" ] && NEEDS_BUILD=1
[ -n "$(find public -type f ! -name 'service-worker.js' -newer "$BUILD_DIR/index.html" 2>/dev/null)" ] && NEEDS_BUILD=1
if [ "$NEEDS_BUILD" -eq 1 ]; then
    echo "📦 build/ устарел — собираю фронтенд..."
    npm run build || { echo "❌ Ошибка сборки фронтенда"; exit 1; }
    echo "✅ Сборка завершена"
fi

# react-scripts затирает наш автономный SW (workbox требует Google CDN) — возвращаем
if [ -f "public/service-worker.js" ]; then
    cp -f "public/service-worker.js" "$BUILD_DIR/service-worker.js"
fi

# ---------- Certificates ----------
if [ ! -f "$CA_CRT" ] || [ ! -f "$CERT_FILE" ] || [ ! -f "$KEY_FILE" ]; then
    echo "🔐 Генерирую CA и серверный сертификат..."
    ALT="DNS:localhost,DNS:skud.local,IP:127.0.0.1"
    [ -n "$IP" ] && ALT="$ALT,IP:$IP"
    openssl genrsa -out "certs/ca.key" 2048 2>/dev/null || { echo "❌ CA key"; exit 1; }
    openssl req -x509 -new -nodes -key "certs/ca.key" -sha256 -days 3650 \
        -out "certs/ca.crt" -subj "/CN=SKUD Local CA" 2>/dev/null || { echo "❌ CA crt"; exit 1; }
    openssl genrsa -out "$KEY_FILE" 2048 2>/dev/null || { echo "❌ server key"; exit 1; }
    openssl req -new -key "$KEY_FILE" -out "certs/server.csr" \
        -subj "/CN=$IP" 2>/dev/null || { echo "❌ CSR"; exit 1; }
    printf "subjectAltName=%s\n" "$ALT" > "certs/san.ext"
    openssl x509 -req -in "certs/server.csr" -CA "certs/ca.crt" -CAkey "certs/ca.key" \
        -CAcreateserial -out "$CERT_FILE" -days 3650 -sha256 -extfile "certs/san.ext" \
        2>/dev/null || { echo "❌ sign server.crt"; exit 1; }
    rm -f "certs/server.csr" "certs/san.ext" "certs/ca.srl"
    echo "✅ Сертификаты готовы"
fi
cp -f "$CA_CRT" "$BUILD_DIR/ca.crt" 2>/dev/null
# ---------- Go backend ----------
BACKEND_STARTED=0
BACKEND_PID=""
if lsof -nP -iTCP:$BACKEND_PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "✅ Go-бэкенд уже запущен на :$BACKEND_PORT"
else
    # Пересобираем, если бинарника нет или исходники новее бинарника
    if [ ! -x "backend/skud-server" ] || [ -n "$(find backend -name '*.go' -newer backend/skud-server 2>/dev/null)" ]; then
        echo "🔨 Собираю бэкенд (go build)..."
        (cd backend && go build -o skud-server .) || { echo "❌ Ошибка сборки бэкенда"; exit 1; }
        echo "✅ Бэкенд собран"
    fi
    echo "▶️  Запускаю Go-бэкенд на :$BACKEND_PORT..."
    cd "$PROJ_DIR/backend"
    nohup ./skud-server > "$LOGS_DIR/backend.log" 2>&1 &
    BACKEND_PID=$!
    disown "$BACKEND_PID" 2>/dev/null
    cd "$PROJ_DIR"
    BACKEND_STARTED=1
    sleep 1
fi

# ---------- HTTP :3000 (только ca.crt) ----------
HTTP_PID=""
if lsof -nP -iTCP:$HTTP_PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "✅ HTTP-сервер уже запущен на :$HTTP_PORT (ca.crt доступен)"
else
    echo "📥 HTTP-сервер для ca.crt на :$HTTP_PORT..."
    HTTP_PORT="$HTTP_PORT" BUILD_DIR="$BUILD_DIR" nohup python3 -c '
import http.server, socketserver, os

PORT = int(os.environ.get("HTTP_PORT", "3000"))
BUILD_DIR = os.environ.get("BUILD_DIR", "build")
CA_FILE = os.path.join(BUILD_DIR, "ca.crt")

class CAHandler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        path = self.path.split("?")[0].rstrip("/")
        if path in ("", "/ca.crt"):
            if not os.path.exists(CA_FILE):
                self.send_error(500, "ca.crt not found")
                return
            with open(CA_FILE, "rb") as f:
                data = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "application/x-x509-ca-cert")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        else:
            self.send_error(404, "Only /ca.crt is available")

    def log_message(self, fmt, *args):
        pass

with socketserver.TCPServer(("0.0.0.0", PORT), CAHandler) as httpd:
    print("CA HTTP server on :%d" % PORT, flush=True)
    httpd.serve_forever()
' > "$LOGS_DIR/ca_server.log" 2>&1 &
    HTTP_PID=$!
    sleep 1
    echo "✅ HTTP-сервер запущен на :$HTTP_PORT"
fi
# ---------- HTTPS :3443 (PWA + API proxy) ----------
HTTPS_PID=""
if lsof -nP -iTCP:$HTTPS_PORT -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo "✅ HTTPS-сервер уже запущен на :$HTTPS_PORT"
else
    echo "🚀 HTTPS PWA сервер на :$HTTPS_PORT..."
    HTTPS_PORT="$HTTPS_PORT" BACKEND_PORT="$BACKEND_PORT" BUILD_DIR="$BUILD_DIR" \
    CERT_FILE="$CERT_FILE" KEY_FILE="$KEY_FILE" \
    nohup python3 -c '
import http.server, ssl, os, json, urllib.parse, urllib.request, urllib.error

PORT = int(os.environ.get("HTTPS_PORT", "3443"))
BUILD_DIR = os.environ.get("BUILD_DIR", "build")
BACKEND = "http://127.0.0.1:" + os.environ.get("BACKEND_PORT", "8080")
CERT = os.environ.get("CERT_FILE", "certs/server.crt")
KEY = os.environ.get("KEY_FILE", "certs/server.key")
PROXY_HEADERS = ("Content-Type", "Authorization", "Accept")

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=BUILD_DIR, **kw)

    def do_GET(self):
        if self.path.startswith("/api/"):
            return self._proxy("GET")
        # SPA-fallback: реальные файлы отдаём как есть, а маршруты BrowserRouter
        # (/dashboard, /login, /admin, /) — это index.html. Без этого F5 на
        # внутреннем маршруте возвращал 404.
        root = os.path.abspath(BUILD_DIR)
        rel = urllib.parse.unquote(self.path.split("?")[0]).lstrip("/")
        full = os.path.normpath(os.path.join(root, rel))
        is_file = bool(rel) and full.startswith(root) and os.path.isfile(full)
        # Не-файлы отдаём оболочкой только браузерной навигации (Accept: text/html),
        # чтобы отсутствующий бандл честно возвращал 404, а не HTML.
        if is_file or "text/html" not in self.headers.get("Accept", ""):
            return super().do_GET()
        original, self.path = self.path, "/index.html"
        try:
            return super().do_GET()
        finally:
            self.path = original

    def do_POST(self):
        if self.path.startswith("/api/"):
            return self._proxy("POST")
        self.send_error(404)

    def do_PUT(self):
        if self.path.startswith("/api/"):
            return self._proxy("PUT")
        self.send_error(404)

    def do_DELETE(self):
        if self.path.startswith("/api/"):
            return self._proxy("DELETE")
        self.send_error(404)

    def _proxy(self, method):
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length) if length else None
        req = urllib.request.Request(BACKEND + self.path, data=body, method=method)
        for h in PROXY_HEADERS:
            v = self.headers.get(h)
            if v:
                req.add_header(h, v)
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                data = r.read()
                self.send_response(r.status)
                for k, v in r.headers.items():
                    if k.lower() not in ("transfer-encoding", "connection", "content-length"):
                        self.send_header(k, v)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
        except urllib.error.HTTPError as e:
            data = e.read()
            self.send_response(e.code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except Exception as e:
            msg = json.dumps({"error": str(e)}).encode()
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(msg)))
            self.end_headers()
            self.wfile.write(msg)

    def end_headers(self):
        # SW и HTML-оболочку не кэшируем по HTTP, иначе телефон застревает на старой сборке
        if self.path in ("/service-worker.js", "/index.html", "/"):
            self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
        super().end_headers()

    def log_message(self, fmt, *args):
        pass

ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(CERT, KEY)
httpd = http.server.ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
print("HTTPS server on :%d" % PORT, flush=True)
httpd.serve_forever()
' > "$LOGS_DIR/https_server.log" 2>&1 &
    HTTPS_PID=$!
    sleep 2
    echo "✅ HTTPS PWA сервер запущен на :$HTTPS_PORT"
fi
# ---------- Инфо ----------
echo ""
echo "==========================================="
echo "  Готово!"
echo "==========================================="
echo ""
if [ -n "$IP" ]; then
    echo "📱 УСТАНОВКА НА ANDROID (Chrome):"
    echo ""
    echo "   ШАГ 1 — установите сертификат на телефон (один раз):"
    echo "      1) Откройте в браузере телефона:  http://$IP:$HTTP_PORT/ca.crt"
    echo "         (файл ca.crt скачается)"
    echo "      2) Настройки Android → Безопасность → Шифрование и учётные данные"
    echo "         → Установить сертификат → Сертификат ЦС → выберите ca.crt"
    echo "         → имя: SKUD → подтвердите (может потребоваться PIN экрана)"
    echo "      ВАЖНО: без этого шага Chrome считает сайт небезопасным"
    echo "      и НЕ даёт установить приложение!"
    echo ""
    echo "   ШАГ 2 — откройте приложение:"
    echo "      https://$IP:$HTTPS_PORT"
    echo ""
    echo "   ШАГ 3 — установите ярлык:"
    echo "      Меню Chrome (⋮) → «Добавить на главный экран» / «Установить приложение»"
    echo "      → ярлык БЕЗ иконки браузера (standalone-приложение)"
    echo ""
else
    echo " Локальный IP не определён, откройте: https://localhost:$HTTPS_PORT"
    echo ""
fi
echo "Бэкенд API : http://localhost:$BACKEND_PORT/api"
echo "PWA (HTTPS): https://localhost:$HTTPS_PORT"
echo "CA (HTTP)  : http://localhost:$HTTP_PORT/ca.crt"
echo "Логи       : $LOGS_DIR/"
echo ""
echo "Нажмите Ctrl+C для остановки всех серверов"
echo ""

# ---------- Остановка ----------
STOPPING=0
stop_all() {
    [ "$STOPPING" -eq 1 ] && return
    STOPPING=1
    echo ""
    echo "Останавливаю серверы..."
    if [ -n "$HTTP_PID" ]; then
        kill "$HTTP_PID" 2>/dev/null && echo "⏹️  HTTP-сервер (:$HTTP_PORT) остановлен"
    fi
    if [ -n "$HTTPS_PID" ]; then
        kill "$HTTPS_PID" 2>/dev/null && echo "⏹️  HTTPS-сервер (:$HTTPS_PORT) остановлен"
    fi
    if [ "$BACKEND_STARTED" -eq 1 ] && [ -n "$BACKEND_PID" ]; then
        kill "$BACKEND_PID" 2>/dev/null && echo "⏹️  Go-бэкенд (:$BACKEND_PORT) остановлен"
    fi
    echo "✅ Все серверы остановлены"
}
trap stop_all EXIT INT TERM

wait