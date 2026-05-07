#!/usr/bin/env node
// Downloads the casper-mcp binary and auto-wires it into every MCP client we
// can detect on the system. The user does not have to run `casper-mcp init`.

const https = require("https");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { execFileSync } = require("child_process");

const REPO = "ASHUTOSH-SWAIN-GIT/casper-mcp";
const BIN_DIR = path.join(__dirname, "bin");
const IS_WINDOWS = process.platform === "win32";
const BIN_PATH = path.join(BIN_DIR, IS_WINDOWS ? "casper-mcp.exe" : "casper-mcp");

const SERVER_ARGS = ["serve", "--dir", ".", "--html", "casper/graph.html"];

const SLASH_COMMAND = `Use the casper MCP tools to build infrastructure context for this project.

$ARGUMENTS

Instructions:
- ALWAYS call render_graph first. This materializes casper/graph.html for the current repo (the file does not exist until you do this). The response includes the absolute path and the directory that was scanned — surface both to the user.
- After rendering, if a specific intent or resource was provided in $ARGUMENTS, call get_context with that intent to find relevant resources, dependencies, and examples.
- If no intent was provided, call dump_graph for a full snapshot, then summarise: total resources, resource types, any policy violations.
- Briefly describe what you found — resource names, types, dependencies, anything notable (drift, policy violations, workflow decisions).
- If the user wants to make a change, call simulate_impact with the proposed HCL before applying anything.
- After the first render_graph call, the file auto-updates whenever .tf files change in the scanned directory.
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

// Resolve the absolute path to the casper-mcp executable so that GUI apps
// (Electron, etc.) can find it even when /opt/homebrew/bin is not on their PATH.
function resolveBinaryPath() {
  if (fs.existsSync(BIN_PATH)) return BIN_PATH;
  try {
    const which = IS_WINDOWS ? "where" : "which";
    return execFileSync(which, ["casper-mcp"], { encoding: "utf8" }).trim().split("\n")[0];
  } catch (_) {
    return "casper-mcp";
  }
}

function installSlashCommand() {
  try {
    const claudeDir = path.join(os.homedir(), ".claude", "commands");
    fs.mkdirSync(claudeDir, { recursive: true });
    fs.writeFileSync(path.join(claudeDir, "casper.md"), SLASH_COMMAND);
    console.log("casper-mcp: /casper slash command installed for Claude Code");
  } catch (_) { /* non-fatal */ }
}

// Merge the casper entry into a JSON-format MCP config, preserving any other
// servers the user already has registered.
function mergeJSONConfig(configPath) {
  let existing = {};
  if (fs.existsSync(configPath)) {
    try {
      existing = JSON.parse(fs.readFileSync(configPath, "utf8")) || {};
    } catch (_) { /* corrupted file → overwrite cleanly */ }
  }
  const servers = existing.mcpServers || {};
  servers.casper = {
    command: resolveBinaryPath(),
    args: SERVER_ARGS,
  };
  existing.mcpServers = servers;
  fs.mkdirSync(path.dirname(configPath), { recursive: true });
  fs.writeFileSync(configPath, JSON.stringify(existing, null, 2) + "\n");
}

function installClaudeCodeUserScope() {
  // Prefer the official `claude mcp add-json` if Claude Code CLI is installed —
  // it lands in the version-correct user-scope file (~/.claude.json today).
  try {
    const config = {
      type: "stdio",
      command: resolveBinaryPath(),
      args: SERVER_ARGS,
    };
    execFileSync("claude", [
      "mcp",
      "add-json",
      "casper",
      JSON.stringify(config),
      "--scope",
      "user",
    ], { stdio: "ignore" });
    console.log("casper-mcp: registered with Claude Code (user MCP scope)");
  } catch (_) {
    // Claude Code CLI not present or registration failed — skip silently.
  }
}

function installClaudeDesktop() {
  let configPath;
  if (process.platform === "darwin") {
    configPath = path.join(os.homedir(), "Library", "Application Support", "Claude", "claude_desktop_config.json");
  } else if (process.platform === "win32") {
    configPath = path.join(process.env.APPDATA || path.join(os.homedir(), "AppData", "Roaming"), "Claude", "claude_desktop_config.json");
  } else {
    configPath = path.join(os.homedir(), ".config", "Claude", "claude_desktop_config.json");
  }

  // Only register if Claude Desktop's directory exists — otherwise we'd litter
  // the filesystem on machines that don't run Claude Desktop.
  if (!fs.existsSync(path.dirname(configPath))) return;

  try {
    mergeJSONConfig(configPath);
    console.log("casper-mcp: registered with Claude Desktop");
  } catch (_) { /* non-fatal */ }
}

function installCursor() {
  const cursorDir = path.join(os.homedir(), ".cursor");
  if (!fs.existsSync(cursorDir)) return;
  try {
    mergeJSONConfig(path.join(cursorDir, "mcp.json"));
    console.log("casper-mcp: registered with Cursor (user scope)");
  } catch (_) { /* non-fatal */ }
}

function installCodex() {
  try {
    const configPath = path.join(os.homedir(), ".codex", "config.toml");
    const cmd = resolveBinaryPath().replace(/\\/g, "\\\\");
    const argsToml = SERVER_ARGS.map((a) => `"${a}"`).join(", ");
    const block = `[mcp_servers.casper]\ncommand = "${cmd}"\nargs = [${argsToml}]\n`;

    fs.mkdirSync(path.dirname(configPath), { recursive: true });

    if (!fs.existsSync(configPath)) {
      fs.writeFileSync(configPath, block);
    } else {
      const text = fs.readFileSync(configPath, "utf8");
      const idx = text.indexOf("[mcp_servers.casper]");
      if (idx === -1) {
        fs.appendFileSync(configPath, (text.endsWith("\n") ? "\n" : "\n\n") + block);
      } else {
        // Replace the existing block so the args/path get updated.
        const headerLen = "[mcp_servers.casper]".length;
        const tail = text.slice(idx + headerLen);
        const next = tail.indexOf("\n[");
        const end = next === -1 ? text.length : idx + headerLen + next + 1;
        fs.writeFileSync(configPath, text.slice(0, idx) + block + text.slice(end));
      }
    }
    console.log("casper-mcp: registered in ~/.codex/config.toml");
  } catch (_) { /* non-fatal */ }
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
  installClaudeCodeUserScope();
  installClaudeDesktop();
  installCursor();
  installCodex();

  console.log("casper-mcp: setup complete — restart your MCP client to use Casper");
}

main().catch((err) => {
  const { version } = require("./package.json");
  process.stderr.write(
    `\ncasper-mcp: binary download failed — ${err.message}\n` +
    `\n` +
    `  Option 1 — retry the install:\n` +
    `    npm install -g casper-mcp\n` +
    `\n` +
    `  Option 2 — install via Go:\n` +
    `    go install github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/cmd/casper-mcp@v${version}\n` +
    `\n` +
    `  Option 3 — download manually:\n` +
    `    https://github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/releases/tag/v${version}\n` +
    `\n` +
    `  After installing, restart Claude Code (or your MCP client).\n\n`
  );
  process.exit(0); // non-fatal: don't fail npm install pipelines
});
