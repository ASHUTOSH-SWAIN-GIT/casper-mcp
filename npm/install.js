#!/usr/bin/env node
// Downloads the casper-mcp binary and installs the /casper Claude Code slash command.

const https = require("https");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { execFileSync } = require("child_process");

const REPO = "ASHUTOSH-SWAIN-GIT/casper-mcp";
const BIN_DIR = path.join(__dirname, "bin");
const IS_WINDOWS = process.platform === "win32";
const BIN_PATH = path.join(BIN_DIR, IS_WINDOWS ? "casper-mcp.exe" : "casper-mcp");

const SLASH_COMMAND = `Use the casper MCP tools to build infrastructure context for this project.

$ARGUMENTS

Instructions:
- If a specific intent or resource was provided in $ARGUMENTS, call get_context with that intent to find relevant resources, dependencies, and examples.
- If no intent was provided, call dump_graph to get a full snapshot of the infrastructure graph, then summarise: total resources, resource types, and any policy violations.
- After getting context, briefly describe what you found — resource names, types, dependencies, and anything notable (drift, policy violations, workflow decisions).
- If the user wants to make a change, call simulate_impact with the proposed HCL before applying anything.
`;

function platformKey() {
  const plt = { darwin: "darwin", linux: "linux", win32: "windows" }[process.platform];
  const cpu = { x64: "amd64", arm64: "arm64" }[process.arch];
  if (!plt || !cpu) throw new Error(`Unsupported platform: ${process.platform}/${process.arch}`);
  return { os: plt, cpu };
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

function installSlashCommand() {
  try {
    const claudeDir = path.join(os.homedir(), ".claude", "commands");
    fs.mkdirSync(claudeDir, { recursive: true });
    fs.writeFileSync(path.join(claudeDir, "casper.md"), SLASH_COMMAND);
    console.log("casper-mcp: /casper slash command installed for Claude Code");
  } catch (e) {
    // Non-fatal
  }
}

function installClaudeConfig() {
  try {
    const settingsPath = path.join(os.homedir(), ".claude", "settings.json");
    let settings = {};
    if (fs.existsSync(settingsPath)) {
      try { settings = JSON.parse(fs.readFileSync(settingsPath, "utf8")); } catch (_) {}
    }
    settings.mcpServers = settings.mcpServers || {};
    settings.mcpServers.casper = { command: "casper-mcp", args: ["serve", "--dir", "."] };
    fs.mkdirSync(path.dirname(settingsPath), { recursive: true });
    fs.writeFileSync(settingsPath, JSON.stringify(settings, null, 2));
    console.log("casper-mcp: registered in ~/.claude/settings.json (Claude Code)");
  } catch (e) { /* non-fatal */ }
}

function installCodexConfig() {
  try {
    const configPath = path.join(os.homedir(), ".codex", "config.toml");
    const entry = `\n[mcp_servers.casper]\ncommand = "casper-mcp"\nargs = ["serve", "--dir", "."]\n`;

    fs.mkdirSync(path.dirname(configPath), { recursive: true });

    if (fs.existsSync(configPath)) {
      const existing = fs.readFileSync(configPath, "utf8");
      if (existing.includes("[mcp_servers.casper]")) return; // already configured
      fs.appendFileSync(configPath, entry);
    } else {
      fs.writeFileSync(configPath, entry.trimStart());
    }
    console.log("casper-mcp: registered in ~/.codex/config.toml (Codex)");
  } catch (e) { /* non-fatal */ }
}

async function main() {
  if (!fs.existsSync(BIN_PATH)) {
    const { version } = require("./package.json");
    const { os: plt, cpu } = platformKey();
    const ext = plt === "windows" ? "zip" : "tar.gz";
    const archive = `casper-mcp_${version}_${plt}_${cpu}.${ext}`;
    const url = `https://github.com/${REPO}/releases/download/v${version}/${archive}`;

    console.log(`casper-mcp: downloading v${version} for ${plt}/${cpu}...`);
    fs.mkdirSync(BIN_DIR, { recursive: true });

    const tmpPath = path.join(BIN_DIR, `_tmp.${ext}`);
    try {
      await download(url, tmpPath);

      if (plt === "windows") {
        execFileSync("powershell", [
          "-Command",
          `Expand-Archive -Path "${tmpPath}" -DestinationPath "${BIN_DIR}" -Force`,
        ]);
      } else {
        execFileSync("tar", ["xzf", tmpPath, "-C", BIN_DIR, "casper-mcp"]);
        fs.chmodSync(BIN_PATH, 0o755);
      }

      console.log(`casper-mcp: binary ready`);
    } finally {
      if (fs.existsSync(tmpPath)) fs.unlinkSync(tmpPath);
    }
  }

  installSlashCommand();
  installClaudeConfig();
  installCodexConfig();
}

main().catch((err) => {
  console.warn(`casper-mcp: install warning: ${err.message}`);
  process.exit(0);
});
