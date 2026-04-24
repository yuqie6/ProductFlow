import { createServer, request as httpRequest } from 'node:http';
import { request as httpsRequest } from 'node:https';
import { readFile } from 'node:fs/promises';
import { createReadStream, existsSync } from 'node:fs';
import { extname, join, normalize, resolve } from 'node:path';
import { URL } from 'node:url';

const rootArg = process.argv[2];
const portArg = process.argv[3];

if (!rootArg) {
  console.error('Usage: node serve_spa_preview.mjs <dist-dir> [port]');
  process.exit(1);
}

const rootDir = resolve(rootArg);
const port = Number(portArg || process.env.PORT || 29281);
const apiTarget = new URL(process.env.API_TARGET || 'http://127.0.0.1:29280');

const MIME_TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.mjs': 'application/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.txt': 'text/plain; charset=utf-8',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
};

function safePathname(pathname) {
  const decoded = decodeURIComponent(pathname.split('?')[0]);
  const normalized = normalize(decoded).replace(/^\/+/, '');
  return normalized;
}

function sendFile(res, filePath) {
  const ext = extname(filePath).toLowerCase();
  res.writeHead(200, {
    'Content-Type': MIME_TYPES[ext] || 'application/octet-stream',
    'Cache-Control': ext === '.html' ? 'no-cache' : 'public, max-age=31536000, immutable',
  });
  createReadStream(filePath).pipe(res);
}

function proxyApi(req, res) {
  const requester = apiTarget.protocol === 'https:' ? httpsRequest : httpRequest;
  const upstreamHeaders = { ...req.headers };
  delete upstreamHeaders.host;
  delete upstreamHeaders.connection;
  delete upstreamHeaders['content-length'];

  const upstreamReq = requester(
    {
      protocol: apiTarget.protocol,
      hostname: apiTarget.hostname,
      port: apiTarget.port || (apiTarget.protocol === 'https:' ? 443 : 80),
      method: req.method,
      path: req.url,
      headers: upstreamHeaders,
    },
    (upstreamRes) => {
      const headers = { ...upstreamRes.headers };
      delete headers.connection;
      res.writeHead(upstreamRes.statusCode || 502, headers);
      upstreamRes.pipe(res);
    },
  );

  upstreamReq.on('error', (error) => {
    res.writeHead(502, { 'Content-Type': 'text/plain; charset=utf-8' });
    res.end(`API proxy error: ${error.message}`);
  });

  req.pipe(upstreamReq);
}

const server = createServer(async (req, res) => {
  try {
    const pathname = req.url || '/';

    if (pathname.startsWith('/api')) {
      return proxyApi(req, res);
    }

    const relativePath = safePathname(pathname === '/' ? '/index.html' : pathname);
    const candidate = join(rootDir, relativePath);

    if (candidate.startsWith(rootDir) && existsSync(candidate)) {
      return sendFile(res, candidate);
    }

    const indexPath = join(rootDir, 'index.html');
    const html = await readFile(indexPath);
    res.writeHead(200, {
      'Content-Type': 'text/html; charset=utf-8',
      'Cache-Control': 'no-cache',
    });
    res.end(html);
  } catch (error) {
    res.writeHead(500, { 'Content-Type': 'text/plain; charset=utf-8' });
    res.end(`Preview server error: ${error instanceof Error ? error.message : String(error)}`);
  }
});

server.listen(port, '0.0.0.0', () => {
  console.log(`Serving ${rootDir} on http://0.0.0.0:${port} with API proxy ${apiTarget.origin}`);
});
