#!/bin/bash
# Simple script to serve the PWA build folder for local network access

echo "=========================================="
echo "  СКУД PWA Server"
echo "=========================================="
echo ""

# Get the local IP address
IP=$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || hostname -I | awk '{print $1}')

if [ -z "$IP" ]; then
    echo "❌ Не удалось определить IP-адрес"
    echo "Укажите IP вручную:"
    read IP
fi

echo "🌐 Локальный IP: $IP"
echo "📱 Откройте на Android:"
echo "   http://$IP:3000"
echo ""
echo "На Android Chrome:"
echo "1. Откройте http://$IP:3000"
echo "2. Меню → 'Добавить на главный экран'"
echo ""
echo "=========================================="
echo "Запуск сервера на порту 3000..."
echo "=========================================="
echo ""

# Check if build folder exists and has index.html
if [ ! -f "build/index.html" ]; then
    echo "⚠️  Файл build/index.html не найден!"
    echo "▶️  Запускаю сборку проекта (npm run build)..."
    echo ""
    npm run build
    echo ""
    if [ ! -f "build/index.html" ]; then
        echo "❌ Сборка не удалась!"
        echo "Проверьте ошибки выше."
        exit 1
    fi
    echo "✅ Сборка завершена"
    echo ""
fi

# Use Python HTTP server with SPA support (redirects all requests to index.html)
cd build

# Check if Python 3 is available
if command -v python3 &> /dev/null; then
    echo "✅ Используется Python 3 HTTP сервер"
    echo ""
    
    # Create a simple SPA server script
    cat > spa_server.py << 'PYTHON_EOF'
#!/usr/bin/env python3
import http.server
import socketserver
import os
import sys

IP = os.environ.get('SPA_IP', '0.0.0.0')

class SPAHandler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        # Serve index.html for all paths (SPA routing)
        if not os.path.exists(self.translate_path(self.path)) or os.path.isdir(self.translate_path(self.path)):
            self.path = '/index.html'
        super().do_GET()
    def log_message(self, format, *args):
        sys.stderr.write("  [req] %s\n" % (format % args))

# Try to find a free port starting from 3000, up to 20 attempts
port = 3000
httpd = None
for attempt in range(20):
    try:
        httpd = socketserver.TCPServer(("", port), SPAHandler)
        break
    except OSError:
        port += 1

if httpd is None:
    print("❌ Не удалось найти свободный порт (3000-3019)")
    sys.exit(1)

print(f"\n🚀 Сервер запущен: http://{IP}:{port}")
print(f"📱 Для Android: http://{IP}:{port}")
print("")
print("Нажмите Ctrl+C для остановки")
print("")
try:
    httpd.serve_forever()
except KeyboardInterrupt:
    print("\n\n⏹️  Сервер остановлен")
PYTHON_EOF
    
    SPA_IP=$IP python3 spa_server.py
else
    echo "⚠️  Python 3 не найден, используем npx serve"
    echo ""
    cd ..
    # Find available port for npx serve
    PORT=3000
    while lsof -Pi :$PORT -sTCP:LISTEN -t >/dev/null 2>&1 ; do
        PORT=$((PORT+1))
    done
    npx serve -s build -l $PORT
fi