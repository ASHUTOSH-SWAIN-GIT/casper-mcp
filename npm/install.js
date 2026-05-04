#!/usr/bin/env node
// Downloads the casper-mcp binary from GitHub releases for the current platform.

const https = require("https");
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const { createGunzip } = require("zlib");

const REPO = "ASHUTOSH-SWAIN-GIT/casper-mcp";
const BIN_DIR = path.join(__dirname, "bin");
const IS_WINDOWS = process.platform === "win32";
const BIN_PATH = path.join(BIN_DIR, IS_WINDOWS ? "casper-mcp.exe" : "casper-mcp");

function platformKey() {
  const os = { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform];
  const cpu = { x64: "amd64", arm64: "arm64" }[process.arch];
  if (!os || !cpu) throw new Error(`Unsupported platform: ${process.platform}/${process.arch}`);
  return { os, cpu };
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const follow = (u) => {
      https.get(u, { headers: { "User-Agent": "casper-mcp-installer" } }, (res) => {
        if (res.statusCode === 301 || res.statusCode === 302) return follow(res.headers.location);
        if (res.statusCode !== 200) return reject(new Error(`HTTP ${res.statusCode} for ${u}`));
        const out = fs.createWriteStream(dest);
        res.pipe(out);
        out.on("finish", resolve);
        out.on("error", reject);
      }).on("error", reject);
    };
    follow(url);
  });
}

function extractTarGz(archive, destDir, binaryName) {
  return new Promise((resolve, reject) => {
    // Pipe through gunzip then use tar (available on all supported platforms)
    try {
      execFileSync("tar", ["xzf", archive, "-C", destDir, binaryName], { stdio: "inherit" });
      resolve();
    } catch (e) {
      reject(e);
    }
  });
}

async function main() {
  if (fs.existsSync(BIN_PATH)) return;

  const { version } = require("./package.json");
  const { os, cpu } = platformKey();
  const ext = os === "windows" ? "zip" : "tar.gz";
  const archive = `casper-mcp_${version}_${os}_${cpu}.${ext}`;
  const url = `https://github.com/${REPO}/releases/download/v${version}/${archive}`;

  console.log(`casper-mcp: downloading v${version} for ${os}/${cpu}...`);
  fs.mkdirSync(BIN_DIR, { recursive: true });

  const tmpPath = path.join(BIN_DIR, `_tmp.${ext}`);
  try {
    await download(url, tmpPath);

    if (os === "windows") {
      execFileSync("powershell", [
        "-Command",
        `Expand-Archive -Path "${tmpPath}" -DestinationPath "${BIN_DIR}" -Force`,
      ]);
    } else {
      execFileSync("tar", ["xzf", tmpPath, "-C", BIN_DIR, "casper-mcp"]);
      fs.chmodSync(BIN_PATH, 0o755);
    }

    console.log(`casper-mcp: ready at ${BIN_PATH}`);
    console.log(`casper-mcp: run "casper-mcp init --global" to add the /casper slash command to Claude Code`);
  } finally {
    if (fs.existsSync(tmpPath)) fs.unlinkSync(tmpPath);
  }
}

main().catch((err) => {
  console.warn(`casper-mcp: install warning (binary will be missing): ${err.message}`);
  process.exit(0); // non-fatal — don't break npm install
});
