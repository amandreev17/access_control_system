#!/usr/bin/env node
/**
 * SKUD PWA Server — кроссплатформенный запуск всего одной командой.
 *
 * Работает на macOS, Linux и Windows БЕЗ WSL, без bash, без python и без openssl:
 * нужны только Node.js (уже требуется для сборки фронтенда) и Go (для бэкенда).
 *
 *   1) Go-бэкенд        :8080  (API: /api/login, /api/generate-qr, /api/validate-qr)
 *   2) HTTPS PWA-сервер :3443  (build/ + прокси /api/* на Go-бэкенд)
 *   3) HTTP-сервер      :3000  (только ca.crt — скачать на телефон)
 *
 * Запуск:  node serve.js      (или ./serve.sh, или npm run serve)
 */

'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const net = require('net');
const http = require('http');
const https = require('https');
const crypto = require('crypto');
const { spawn } = require('child_process');

const ROOT = __dirname;
const BUILD_DIR = path.join(ROOT, 'build');
const CERTS_DIR = path.join(ROOT, 'certs');
const LOGS_DIR = path.join(ROOT, 'logs');
const BACKEND_DIR = path.join(ROOT, 'backend');

const BACKEND_PORT = Number(process.env.BACKEND_PORT || 8080);
const HTTPS_PORT = Number(process.env.HTTPS_PORT || 3443);
const HTTP_PORT = Number(process.env.HTTP_PORT || 3000);

const IS_WINDOWS = process.platform === 'win32';
const CA_CRT = path.join(CERTS_DIR, 'ca.crt');
const CA_KEY = path.join(CERTS_DIR, 'ca.key');
const SERVER_CRT = path.join(CERTS_DIR, 'server.crt');
const SERVER_KEY = path.join(CERTS_DIR, 'server.key');

// ------------------------------------------------------------------ утилиты --

const log = (msg) => console.log(msg);

function localIP() {
  const ifaces = os.networkInterfaces();
  const candidates = [];
  for (const name of Object.keys(ifaces)) {
    for (const inf of ifaces[name] || []) {
      if (inf.family !== 'IPv4' || inf.internal) continue;
      if (inf.address.startsWith('169.254.')) continue; // APIPA
      candidates.push(inf.address);
    }
  }
  // Предпочитаем домашние/офисные подсети
  const preferred = candidates.find(
    (ip) => ip.startsWith('192.168.') || ip.startsWith('10.') || /^172\.(1[6-9]|2\d|3[01])\./.test(ip)
  );
  return preferred || candidates[0] || '127.0.0.1';
}

function isPortBusy(port) {
  return new Promise((resolve) => {
    const sock = net.connect({ host: '127.0.0.1', port });
    const done = (busy) => {
      sock.destroy();
      resolve(busy);
    };
    sock.setTimeout(700);
    sock.once('connect', () => done(true));
    sock.once('timeout', () => done(false));
    sock.once('error', () => done(false));
  });
}

function walkFiles(dir, out = []) {
  let entries = [];
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const e of entries) {
    const full = path.join(dir, e.name);
    if (e.isDirectory()) walkFiles(full, out);
    else out.push(full);
  }
  return out;
}

function newestMtime(dir, exclude = () => false) {
  let newest = 0;
  for (const file of walkFiles(dir)) {
    if (exclude(file)) continue;
    try {
      const t = fs.statSync(file).mtimeMs;
      if (t > newest) newest = t;
    } catch {
      /* ignore */
    }
  }
  return newest;
}

function mtimeOf(file) {
  try {
    return fs.statSync(file).mtimeMs;
  } catch {
    return 0;
  }
}

function tee(file, line) {
  try {
    fs.appendFileSync(file, line + '\n');
  } catch {
    /* ignore */
  }
}

// -------------------------------------------- сертификаты (чистый Node, без openssl) --

const derLen = (n) => {
  if (n < 0x80) return Buffer.from([n]);
  const bytes = [];
  let v = n;
  while (v > 0) {
    bytes.unshift(v & 0xff);
    v = Math.floor(v / 256);
  }
  return Buffer.from([0x80 | bytes.length, ...bytes]);
};
const tlv = (tag, content) => Buffer.concat([Buffer.from([tag]), derLen(content.length), content]);
const SEQ = (c) => tlv(0x30, c);
const SET = (c) => tlv(0x31, c);
const OCTSTR = (c) => tlv(0x04, c);
const BOOL = (b) => tlv(0x01, Buffer.from([b ? 0xff : 0x00]));
const NULL = Buffer.from([0x05, 0x00]);
const BITSTR = (c) => tlv(0x03, Buffer.concat([Buffer.from([0]), c]));
const BITSTR_RAW = (unused, c) => tlv(0x03, Buffer.concat([Buffer.from([unused]), c]));
const ctx = (n, c, constructed = true) => tlv((constructed ? 0xa0 : 0x80) + n, c);
const UTF8STR = (s) => tlv(0x0c, Buffer.from(s, 'utf8'));

function oidBytes(dotted) {
  const parts = dotted.split('.').map(Number);
  const bytes = [40 * parts[0] + parts[1]];
  for (const p of parts.slice(2)) {
    const stack = [p & 0x7f];
    let v = Math.floor(p / 128);
    while (v > 0) {
      stack.unshift((v & 0x7f) | 0x80);
      v = Math.floor(v / 128);
    }
    bytes.push(...stack);
  }
  return Buffer.from(bytes);
}
const OID = (d) => tlv(0x06, oidBytes(d));

function intFromBuffer(buf) {
  let b = buf;
  while (b.length > 1 && b[0] === 0x00) b = b.subarray(1);
  if (b[0] & 0x80) b = Buffer.concat([Buffer.from([0x00]), b]);
  return tlv(0x02, b);
}
function intFromNumber(n) {
  const bytes = [];
  let v = n;
  do {
    bytes.unshift(v & 0xff);
    v = Math.floor(v / 256);
  } while (v > 0);
  if (bytes[0] & 0x80) bytes.unshift(0x00);
  return tlv(0x02, Buffer.from(bytes));
}
function TIME(date) {
  const p = (x) => String(x).padStart(2, '0');
  const y = date.getUTCFullYear();
  const core =
    p(date.getUTCMonth() + 1) + p(date.getUTCDate()) + p(date.getUTCHours()) +
    p(date.getUTCMinutes()) + p(date.getUTCSeconds()) + 'Z';
  if (y < 2050) return tlv(0x17, Buffer.from(p(y % 100) + core, 'ascii'));
  return tlv(0x18, Buffer.from(String(y) + core, 'ascii'));
}

const ALG_SHA256_RSA = SEQ(Buffer.concat([OID('1.2.840.113549.1.1.11'), NULL]));
const rdn = (cn) => SEQ(SET(SEQ(Buffer.concat([OID('2.5.4.3'), UTF8STR(cn)]))));
const ext = (oid, critical, value) =>
  SEQ(Buffer.concat([OID(oid), critical ? BOOL(true) : Buffer.alloc(0), OCTSTR(value)]));

const ipToBuffer = (ip) => Buffer.from(ip.split('.').map(Number));

function pemToDer(pem) {
  const b64 = String(pem).replace(/-----[^-]+-----/g, '').replace(/\s+/g, '');
  return Buffer.from(b64, 'base64');
}
function derToPem(der, label) {
  const b64 = der.toString('base64');
  return `-----BEGIN ${label}-----\n${b64.match(/.{1,64}/g).join('\n')}\n-----END ${label}-----\n`;
}

function generateKeyPair() {
  return crypto.generateKeyPairSync('rsa', {
    modulusLength: 2048,
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  });
}

function buildCertificate({ subjectCN, issuerCN, isCA, publicKeyPem, signingKeyPem, dns = [], ips = [] }) {
  const tbs = [];
  tbs.push(ctx(0, intFromNumber(2))); // version v3
  tbs.push(intFromBuffer(crypto.randomBytes(16))); // serialNumber
  tbs.push(ALG_SHA256_RSA);
  tbs.push(rdn(issuerCN));
  tbs.push(SEQ(Buffer.concat([
    TIME(new Date(Date.now() - 24 * 3600 * 1000)),
    TIME(new Date(Date.now() + 3650 * 24 * 3600 * 1000)),
  ])));
  tbs.push(rdn(subjectCN));
  tbs.push(pemToDer(publicKeyPem));

  const exts = [];
  exts.push(ext('2.5.29.19', true, SEQ(isCA ? BOOL(true) : Buffer.alloc(0)))); // basicConstraints
  exts.push(ext('2.5.29.15', true, isCA ? BITSTR_RAW(1, Buffer.from([0x06])) : BITSTR_RAW(5, Buffer.from([0xa0]))));
  if (!isCA) {
    exts.push(ext('2.5.29.37', false, SEQ(OID('1.3.6.1.5.5.7.3.1')))); // extKeyUsage: serverAuth
    const names = [];
    for (const d of dns) names.push(ctx(2, Buffer.from(d, 'ascii'), false));
    for (const ip of ips) names.push(ctx(7, ipToBuffer(ip), false));
    exts.push(ext('2.5.29.17', false, SEQ(Buffer.concat(names)))); // subjectAltName
  }
  tbs.push(ctx(3, SEQ(Buffer.concat(exts))));

  const tbsDer = SEQ(Buffer.concat(tbs));
  const signature = crypto.sign('sha256', tbsDer, crypto.createPrivateKey(signingKeyPem));
  return derToPem(SEQ(Buffer.concat([tbsDer, ALG_SHA256_RSA, BITSTR(signature)])), 'CERTIFICATE');
}

function ensureCertificates(ip) {
  fs.mkdirSync(CERTS_DIR, { recursive: true });
  const dns = ['localhost', 'skud.local'];
  const ips = ['127.0.0.1'];
  if (ip && ip !== '127.0.0.1') ips.push(ip);

  // Уже есть валидный сертификат на этот IP — ничего не трогаем
  if (fs.existsSync(SERVER_CRT) && fs.existsSync(SERVER_KEY) && fs.existsSync(CA_CRT)) {
    try {
      const cert = new crypto.X509Certificate(fs.readFileSync(SERVER_CRT));
      const san = cert.subjectAltName || '';
      const dateOk = Date.parse(cert.validTo) > Date.now() + 7 * 24 * 3600 * 1000;
      const ipOk = ips.every((v) => v === '127.0.0.1' || san.includes(`IP Address:${v}`));
      if (dateOk && ipOk) {
        log('✅ Сертификаты актуальны (IP не менялся)');
        return;
      }
    } catch {
      /* перегенерируем */
    }
  }

  let caKeyPem;
  let caCrtPem;
  if (fs.existsSync(CA_KEY) && fs.existsSync(CA_CRT)) {
    // CA сохраняем: на телефоне не нужно устанавливать сертификат заново
    caKeyPem = fs.readFileSync(CA_KEY, 'utf8');
    caCrtPem = fs.readFileSync(CA_CRT, 'utf8');
    log('🔐 Перевыпускаю серверный сертификат (CA сохранён — на телефоне переустанавливать не нужно)');
  } else {
    log('🔐 Создаю CA и серверный сертификат (чистый Node, openssl не нужен)...');
    const ca = generateKeyPair();
    caKeyPem = ca.privateKey;
    caCrtPem = buildCertificate({
      subjectCN: 'SKUD Local CA',
      issuerCN: 'SKUD Local CA',
      isCA: true,
      publicKeyPem: ca.publicKey,
      signingKeyPem: ca.privateKey,
    });
    fs.writeFileSync(CA_KEY, caKeyPem, { mode: 0o600 });
    fs.writeFileSync(CA_CRT, caCrtPem);
  }

  const srv = generateKeyPair();
  const serverCrtPem = buildCertificate({
    subjectCN: ip,
    issuerCN: 'SKUD Local CA',
    isCA: false,
    publicKeyPem: srv.publicKey,
    signingKeyPem: caKeyPem,
    dns,
    ips,
  });
  fs.writeFileSync(SERVER_KEY, srv.privateKey, { mode: 0o600 });
  fs.writeFileSync(SERVER_CRT, serverCrtPem);
  fs.writeFileSync(path.join(CERTS_DIR, 'chain.pem'), serverCrtPem + caCrtPem);
  log('✅ Сертификаты готовы');
}

// ---------------------------------------------------- сборка фронтенда/бэкенда --

const npmCommand = () => (IS_WINDOWS ? 'npm.cmd' : 'npm');

function run(cmd, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, { stdio: 'inherit', ...options });
    child.on('error', reject);
    child.on('close', (code) =>
      code === 0 ? resolve() : reject(new Error(`${cmd} завершился с кодом ${code}`))
    );
  });
}

function frontendNeedsBuild() {
  const indexHtml = path.join(BUILD_DIR, 'index.html');
  if (!fs.existsSync(indexHtml)) return true;
  const built = mtimeOf(indexHtml);
  if (newestMtime(path.join(ROOT, 'src')) > built) return true;
  const pubNewest = newestMtime(path.join(ROOT, 'public'), (f) => path.basename(f) === 'service-worker.js');
  return pubNewest > built;
}

async function buildFrontend() {
  const craBin = path.join(ROOT, 'node_modules', 'react-scripts', 'bin', 'react-scripts.js');
  log('📦 Собираю фронтенд (react-scripts build)...');
  const env = { ...process.env, NODE_OPTIONS: '--openssl-legacy-provider' };
  if (fs.existsSync(craBin)) {
    await run(process.execPath, [craBin, 'build'], { cwd: ROOT, env });
  } else {
    await run(npmCommand(), ['run', 'build'], { cwd: ROOT, env, shell: IS_WINDOWS });
  }
  log('✅ Сборка завершена');
}

// react-scripts затирает наш автономный SW (workbox требует Google CDN) — возвращаем
function restoreServiceWorker() {
  const src = path.join(ROOT, 'public', 'service-worker.js');
  if (fs.existsSync(src)) fs.copyFileSync(src, path.join(BUILD_DIR, 'service-worker.js'));
}

const backendBinName = () => (IS_WINDOWS ? 'skud-server.exe' : 'skud-server');

async function startBackend() {
  if (await isPortBusy(BACKEND_PORT)) {
    log(`✅ Go-бэкенд уже запущен на :${BACKEND_PORT}`);
    return null;
  }
  const binPath = path.join(BACKEND_DIR, backendBinName());
  const binTime = mtimeOf(binPath);
  if (!binTime || newestMtime(BACKEND_DIR, (f) => !f.endsWith('.go')) > binTime) {
    log('🔨 Собираю бэкенд (go build)...');
    try {
      await run('go', ['build', '-o', backendBinName(), '.'], { cwd: BACKEND_DIR });
    } catch (e) {
      log(`❌ Не удалось собрать бэкенд: ${e.message}`);
      log('   Проверьте, что установлен Go (https://go.dev/dl/) и доступен в PATH как "go".');
      throw e;
    }
    log('✅ Бэкенд собран');
  }
  const logFile = path.join(LOGS_DIR, 'backend.log');
  const child = spawn(binPath, [], { cwd: BACKEND_DIR, stdio: ['ignore', 'pipe', 'pipe'] });
  const pipe = (stream, out) => stream.on('data', (d) => { fs.appendFileSync(logFile, d); out.write(d); });
  pipe(child.stdout, process.stdout);
  pipe(child.stderr, process.stderr);
  child.on('error', (e) => log(`❌ Бэкенд не запустился: ${e.message}`));
  await new Promise((r) => setTimeout(r, 1000));
  return child;
}

// ------------------------------------------------------------------ серверы --

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.mjs': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.webmanifest': 'application/manifest+json',
  '.map': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.txt': 'text/plain; charset=utf-8',
  '.crt': 'application/x-x509-ca-cert',
  '.pem': 'application/x-x509-ca-cert',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
};

// Эти файлы нельзя кэшировать по HTTP, иначе телефон застревает на старой сборке
const NO_CACHE_PATHS = new Set(['/', '/index.html', '/service-worker.js']);

function safePath(url) {
  try {
    return decodeURIComponent(new URL(url, 'http://localhost').pathname);
  } catch {
    return new URL(url, 'http://localhost').pathname;
  }
}

function sendFile(res, file, pathname) {
  const size = fs.statSync(file).size;
  const headers = {
    'Content-Type': MIME[path.extname(pathname).toLowerCase()] || 'application/octet-stream',
    'Content-Length': size,
  };
  if (NO_CACHE_PATHS.has(pathname)) headers['Cache-Control'] = 'no-cache, no-store, must-revalidate';
  res.writeHead(200, headers);
  fs.createReadStream(file).pipe(res);
}

function serveStatic(req, res) {
  const pathname = safePath(req.url);
  const rel = path.normalize(pathname).replace(/^[/\\]+/, '');
  const full = path.resolve(BUILD_DIR, rel);
  if (!full.startsWith(path.resolve(BUILD_DIR))) {
    res.writeHead(403, { 'Content-Type': 'text/plain; charset=utf-8' });
    res.end('403 Forbidden');
    return;
  }
  if (fs.existsSync(full) && fs.statSync(full).isFile()) return sendFile(res, full, pathname);

  // SPA-fallback: маршруты BrowserRouter (/dashboard, /login, /admin) — это index.html,
  // иначе F5 на внутреннем маршруте возвращал 404. Корень отдаём всегда.
  const indexHtml = path.join(BUILD_DIR, 'index.html');
  const wantsHtml = pathname === '/' || String(req.headers.accept || '').includes('text/html');
  if (wantsHtml && fs.existsSync(indexHtml)) return sendFile(res, indexHtml, '/index.html');
  res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' });
  res.end('404 Not Found');
}

function proxyAPI(req, res) {
  const headers = { ...req.headers };
  headers.host = `127.0.0.1:${BACKEND_PORT}`;
  delete headers.connection;
  const preq = http.request(
    { host: '127.0.0.1', port: BACKEND_PORT, path: req.url, method: req.method, headers },
    (pres) => {
      res.writeHead(pres.statusCode || 502, pres.headers);
      pres.pipe(res);
    }
  );
  preq.on('error', (e) => {
    if (!res.headersSent) res.writeHead(502, { 'Content-Type': 'application/json; charset=utf-8' });
    res.end(JSON.stringify({ error: `Бэкенд недоступен: ${e.message}` }));
  });
  req.pipe(preq);
}

function withLogging(logFile, handler) {
  return (req, res) => {
    const started = Date.now();
    res.on('finish', () => {
      const ip = String(req.socket.remoteAddress || '').replace(/^::ffff:/, '');
      const line = `${ip} - "${req.method} ${req.url}" ${res.statusCode} - ${Date.now() - started}ms`;
      console.log(`   ${line}`);
      tee(logFile, line);
    });
    handler(req, res);
  };
}

function httpsHandler(req, res) {
  if (safePath(req.url).startsWith('/api/')) return proxyAPI(req, res);
  return serveStatic(req, res);
}

// :3000 отдаёт только ca.crt — его скачивают на телефон до установки доверия
function caHandler(req, res) {
  const pathname = safePath(req.url);
  if (pathname === '/ca.crt' || pathname === '/') {
    if (!fs.existsSync(CA_CRT)) {
      res.writeHead(500, { 'Content-Type': 'text/plain; charset=utf-8' });
      res.end('ca.crt not found');
      return;
    }
    sendFile(res, CA_CRT, '/ca.crt');
    return;
  }
  res.writeHead(404, { 'Content-Type': 'text/plain; charset=utf-8' });
  res.end('404 Not Found');
}

// ------------------------------------------------------------------- запуск --

const servers = [];
const children = [];
let stopping = false;

function stopAll() {
  if (stopping) return;
  stopping = true;
  console.log('\nОстанавливаю серверы...');
  for (const s of servers) {
    try {
      s.close();
    } catch {
      /* ignore */
    }
  }
  for (const c of children) {
    try {
      c.kill();
    } catch {
      /* ignore */
    }
  }
  console.log('✅ Все серверы остановлены');
  setTimeout(() => process.exit(0), 300);
}

function printInfo(ip) {
  const lines = [''];
  lines.push('===========================================');
  lines.push('  Готово!');
  lines.push('===========================================');
  lines.push('');
  if (ip !== '127.0.0.1') {
    lines.push('📱 УСТАНОВКА НА ANDROID (Chrome):');
    lines.push('');
    lines.push('   ШАГ 1 — установите сертификат на телефон (один раз):');
    lines.push(`      1) Откройте в браузере телефона:  http://${ip}:${HTTP_PORT}/ca.crt`);
    lines.push('         (файл ca.crt скачается)');
    lines.push('      2) Настройки Android → Безопасность → Шифрование и учётные данные');
    lines.push('         → Установить сертификат → Сертификат ЦС → выберите ca.crt');
    lines.push('         → имя: SKUD → подтвердите (может потребоваться PIN экрана)');
    lines.push('      ВАЖНО: без этого шага Chrome считает сайт небезопасным');
    lines.push('      и НЕ даёт установить приложение!');
    lines.push('');
    lines.push('   ШАГ 2 — откройте приложение:');
    lines.push(`      https://${ip}:${HTTPS_PORT}`);
    lines.push('');
    lines.push('   ШАГ 3 — установите ярлык:');
    lines.push('      Меню Chrome (⋮) → «Добавить на главный экран» / «Установить приложение»');
    lines.push('');
    lines.push(`   Телефон должен быть в той же Wi-Fi сети, что и компьютер (${ip}).`);
    if (IS_WINDOWS) {
      lines.push('   Windows: разрешите Node.js в брандмауэре для частных сетей (порты 3443, 3000).');
    }
    lines.push('');
  } else {
    lines.push(` Локальный IP не определён, откройте: https://localhost:${HTTPS_PORT}`);
    lines.push('');
  }
  lines.push(`Бэкенд API : http://localhost:${BACKEND_PORT}/api`);
  lines.push(`PWA (HTTPS): https://localhost:${HTTPS_PORT}`);
  lines.push(`CA (HTTP)  : http://localhost:${HTTP_PORT}/ca.crt`);
  lines.push(`Логи       : ${LOGS_DIR}`);
  lines.push('');
  lines.push('Нажмите Ctrl+C для остановки всех серверов');
  lines.push('');
  console.log(lines.join('\n'));
}

async function main() {
  for (const dir of [BUILD_DIR, CERTS_DIR, LOGS_DIR]) fs.mkdirSync(dir, { recursive: true });

  console.log('===========================================');
  console.log('  СКУД — HTTPS PWA');
  console.log('  (запуск без WSL: Windows / macOS / Linux)');
  console.log('===========================================');

  const ip = localIP();
  console.log(`🌐 Локальный IP: ${ip}`);

  if (frontendNeedsBuild()) await buildFrontend();
  restoreServiceWorker();
  ensureCertificates(ip);
  fs.copyFileSync(CA_CRT, path.join(BUILD_DIR, 'ca.crt'));

  const backend = await startBackend();
  if (backend) children.push(backend);

  if (await isPortBusy(HTTPS_PORT)) {
    console.log(`⚠️  Порт ${HTTPS_PORT} уже занят — HTTPS-сервер не запущен`);
  } else {
    const cert = fs.readFileSync(SERVER_CRT, 'utf8') + fs.readFileSync(CA_CRT, 'utf8');
    const server = https.createServer(
      { cert, key: fs.readFileSync(SERVER_KEY, 'utf8') },
      withLogging(path.join(LOGS_DIR, 'https_server.log'), httpsHandler)
    );
    await new Promise((resolve) => server.listen(HTTPS_PORT, '0.0.0.0', resolve));
    servers.push(server);
    console.log(`✅ HTTPS PWA сервер запущен на :${HTTPS_PORT}`);
  }

  if (await isPortBusy(HTTP_PORT)) {
    console.log(`⚠️  Порт ${HTTP_PORT} уже занят — сервер раздачи ca.crt не запущен`);
  } else {
    const server = http.createServer(withLogging(path.join(LOGS_DIR, 'ca_server.log'), caHandler));
    await new Promise((resolve) => server.listen(HTTP_PORT, '0.0.0.0', resolve));
    servers.push(server);
    console.log(`✅ HTTP-сервер (ca.crt) запущен на :${HTTP_PORT}`);
  }

  printInfo(ip);
  process.on('SIGINT', stopAll);
  process.on('SIGTERM', stopAll);
}

main().catch((e) => {
  console.error(`❌ Ошибка запуска: ${e.message}`);
  stopAll();
});


