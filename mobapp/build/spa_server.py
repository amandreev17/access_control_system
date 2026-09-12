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
