#!/usr/bin/env node
// Serves the mock build and the exported snapshot on one loopback port.
// The snapshot is read from the user's output folder, never copied into the
// repository.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import path from "node:path";

const [snapshotPath, distDir] = process.argv.slice(2);
if (snapshotPath === undefined || distDir === undefined) {
  console.error("usage: serve-real-data-shots.mjs <snapshot.json> <dist-mock>");
  process.exit(2);
}

const root = path.resolve(distDir);

const contentTypes = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
};

async function readAsset(relative) {
  const target = path.resolve(root, relative);
  if (target !== root && !target.startsWith(`${root}${path.sep}`)) {
    return undefined;
  }
  try {
    return { target, body: await readFile(target) };
  } catch {
    return undefined;
  }
}

const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    let pathname;
    try {
      pathname = decodeURIComponent(url.pathname);
    } catch {
      response.writeHead(400).end("bad path");
      return;
    }

    if (pathname === "/snapshot.json") {
      const body = await readFile(snapshotPath);
      response.writeHead(200, {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-store",
      });
      response.end(body);
      return;
    }

    const relative =
      pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
    let asset = await readAsset(relative);
    if (asset === undefined && path.extname(relative) === "") {
      asset = await readAsset("index.html");
    }
    if (asset === undefined) {
      response.writeHead(404).end("not found");
      return;
    }

    response.writeHead(200, {
      "Content-Type":
        contentTypes[path.extname(asset.target)] ?? "application/octet-stream",
      "Cache-Control": "no-store",
    });
    response.end(asset.body);
  } catch {
    response.writeHead(500).end("server error");
  }
});

server.listen(0, "127.0.0.1", () => {
  const address = server.address();
  if (address === null || typeof address === "string") {
    console.error("could not bind a loopback port");
    process.exit(1);
  }
  console.log(`PORT=${address.port}`);
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    server.close(() => process.exit(0));
  });
}
