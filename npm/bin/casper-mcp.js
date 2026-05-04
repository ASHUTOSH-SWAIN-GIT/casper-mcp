#!/usr/bin/env node
const { spawnSync } = require("child_process");
const path = require("path");
const fs = require("fs");

const bin = path.join(
  __dirname,
  process.platform === "win32" ? "casper-mcp.exe" : "casper-mcp"
);

if (!fs.existsSync(bin)) {
  // Binary missing — postinstall may have failed or been skipped (e.g. --ignore-scripts).
  // Attempt a silent recovery before giving up. Stdout is suppressed to avoid corrupting
  // the MCP stdio protocol if we're being spawned as an MCP server.
  const installScript = path.join(__dirname, "..", "install.js");
  if (fs.existsSync(installScript)) {
    process.stderr.write("casper-mcp: binary not found — attempting download, please wait...\n");
    spawnSync(process.execPath, [installScript], {
      stdio: ["ignore", "ignore", "inherit"],
    });
  }

  if (!fs.existsSync(bin)) {
    const pkg = (() => { try { return require("../package.json"); } catch { return {}; } })();
    const version = pkg.version || "latest";
    process.stderr.write(
      `\ncasper-mcp: binary still not found after recovery attempt.\n` +
      `\n` +
      `  Fix 1 — re-run install:\n` +
      `    npm install -g casper-mcp\n` +
      `\n` +
      `  Fix 2 — install via Go:\n` +
      `    go install github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/cmd/casper-mcp@v${version}\n` +
      `\n` +
      `  Fix 3 — download manually:\n` +
      `    https://github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/releases/tag/v${version}\n` +
      `\n` +
      `  After installing, restart Claude Code (or your MCP client).\n\n`
    );
    process.exit(1);
  }

  process.stderr.write("casper-mcp: download complete — starting server\n");
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
process.exit(result.status ?? 1);
