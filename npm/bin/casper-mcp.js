#!/usr/bin/env node
const { spawnSync } = require("child_process");
const path = require("path");
const fs = require("fs");

const bin = path.join(
  __dirname,
  process.platform === "win32" ? "casper-mcp.exe" : "casper-mcp"
);

if (!fs.existsSync(bin)) {
  console.error(
    "casper-mcp: binary not found. Re-run: npm install casper-mcp\n" +
    "If the problem persists, download manually from:\n" +
    "  https://github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/releases"
  );
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
process.exit(result.status ?? 1);
