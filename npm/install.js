#!/usr/bin/env node
// Postinstall: detecta SO/arch, baixa binário do GitHub Releases, instala
// em ./bin/nessy. npm cria symlink em node_modules/.bin/nessy automaticamente
// via `bin` no package.json.
//
// Versão do binário = versão deste pacote npm. Mantido em sync via release CI.

const fs = require("fs");
const path = require("path");
const https = require("https");
const { spawnSync } = require("child_process");

const pkg = require("./package.json");
const REPO = "Felipeness/nessy";
const VERSION = "v" + pkg.version;
const BIN_DIR = path.join(__dirname, "bin");

function detectPlatform() {
  const platform = process.platform;
  const arch = process.arch;

  let goos;
  switch (platform) {
    case "darwin": goos = "darwin"; break;
    case "linux":  goos = "linux";  break;
    case "win32":  goos = "windows"; break;
    default:
      throw new Error("SO nao suportado: " + platform);
  }

  let goarch;
  switch (arch) {
    case "x64":   goarch = "amd64"; break;
    case "arm64": goarch = "arm64"; break;
    default:
      throw new Error("Arch nao suportada: " + arch);
  }

  return { goos, goarch };
}

function archiveURL(goos, goarch) {
  const ext = goos === "windows" ? "zip" : "tar.gz";
  const versionNum = pkg.version;
  const fname = "nessy_" + versionNum + "_" + goos + "_" + goarch + "." + ext;
  return "https://github.com/" + REPO + "/releases/download/" + VERSION + "/" + fname;
}

function download(url, dest) {
  return new Promise((resolve, reject) => {
    const handle = (response) => {
      if (response.statusCode === 301 || response.statusCode === 302) {
        return download(response.headers.location, dest).then(resolve).catch(reject);
      }
      if (response.statusCode !== 200) {
        return reject(new Error("HTTP " + response.statusCode + " em " + url));
      }
      const file = fs.createWriteStream(dest);
      response.pipe(file);
      file.on("finish", () => file.close(resolve));
      file.on("error", reject);
    };
    https.get(url, handle).on("error", reject);
  });
}

// Run cmd usando spawnSync (sem shell). args sao array, sem injecao.
function run(cmd, args) {
  const r = spawnSync(cmd, args, { stdio: "inherit" });
  if (r.status !== 0) {
    throw new Error(cmd + " falhou (status " + r.status + ")");
  }
}

function extractTarGz(archivePath, destDir) {
  run("tar", ["-xzf", archivePath, "-C", destDir]);
}

function extractZip(archivePath, destDir) {
  if (process.platform === "win32") {
    run("powershell", [
      "-Command",
      "Expand-Archive",
      "-LiteralPath", archivePath,
      "-DestinationPath", destDir,
      "-Force",
    ]);
  } else {
    run("unzip", ["-q", "-o", archivePath, "-d", destDir]);
  }
}

async function main() {
  if (process.env.NESSY_SKIP_INSTALL === "1") {
    console.log("Nessy: NESSY_SKIP_INSTALL=1, pulando download");
    return;
  }

  const { goos, goarch } = detectPlatform();
  const url = archiveURL(goos, goarch);
  const ext = goos === "windows" ? "zip" : "tar.gz";

  fs.mkdirSync(BIN_DIR, { recursive: true });
  const archive = path.join(BIN_DIR, "nessy." + ext);

  console.log("Nessy " + VERSION + ": baixando " + goos + "/" + goarch + "...");
  console.log("  " + url);

  try {
    await download(url, archive);
  } catch (err) {
    console.error("\nErro ao baixar binario: " + err.message);
    console.error("Releases: https://github.com/" + REPO + "/releases");
    process.exit(1);
  }

  console.log("Extraindo...");
  if (ext === "zip") extractZip(archive, BIN_DIR);
  else extractTarGz(archive, BIN_DIR);

  const binaryName = goos === "windows" ? "nessy.exe" : "nessy";
  const target = path.join(BIN_DIR, "nessy");
  const extracted = path.join(BIN_DIR, binaryName);

  if (extracted !== target && fs.existsSync(extracted)) {
    fs.renameSync(extracted, target);
  }

  if (goos !== "windows") {
    fs.chmodSync(target, 0o755);
  }

  fs.unlinkSync(archive);

  console.log("\n+ Nessy instalado.");
  console.log("  Comando: nessy --help");
}

main().catch((err) => {
  console.error("Nessy install falhou:", err.message);
  process.exit(1);
});
