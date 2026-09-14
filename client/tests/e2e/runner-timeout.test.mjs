import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { runPhase } from "./run.mjs";

function getFreePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.unref();
    s.on("error", reject);
    s.listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });
}

function checkPortResponding(port, timeoutMs = 400) {
  return new Promise((resolve) => {
    const socket = net.createConnection({ port, host: "127.0.0.1" });
    const timer = setTimeout(() => {
      socket.destroy();
      resolve(false);
    }, timeoutMs);
    socket.on("connect", () => {
      clearTimeout(timer);
      socket.destroy();
      resolve(true);
    });
    socket.on("error", () => {
      clearTimeout(timer);
      resolve(false);
    });
  });
}

function isPidAlive(pid) {
  if (!pid || Number.isNaN(pid)) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

test("Runner timeout: encerra árvore de processos descendentes, libera porta TCP e executa teardown limpo", async () => {
  const tmpDir = os.tmpdir();
  const testPort = await getFreePort();
  const phaseName = `timeout-test-${Date.now()}`;
  const infoFile = path.join(tmpDir, `wacalls-e2e-info-${Date.now()}.json`);

  // Script executado pelo runner no lugar do Playwright.
  // O processo pai spawna um processo descendente (neto) que abre um servidor TCP real
  // na porta `testPort` e grava os PIDs em `infoFile` assim que o bind for bem-sucedido.
  const grandchildCode = `
    const http = require("node:http");
    const fs = require("node:fs");
    const s = http.createServer((req, res) => res.end("ok"));
    s.listen(${testPort}, "127.0.0.1", () => {
      fs.writeFileSync(${JSON.stringify(infoFile)}, JSON.stringify({
        parentPid: process.ppid,
        grandchildPid: process.pid,
        port: ${testPort},
      }), "utf8");
    });
    setInterval(() => {}, 1000);
  `;

  const hangingParentScript = `
    const { spawn } = require("node:child_process");
    const child = spawn(process.execPath, ["-e", ${JSON.stringify(grandchildCode)}], {
      stdio: "ignore",
    });
    setInterval(() => {}, 1000);
  `;

  // Dispara a fase em segundo plano com timeout de 5 segundos.
  // O handler de erro é anexado imediatamente para evitar unhandledRejection no Node.
  let runErr = null;
  const runPromise = runPhase({
    name: phaseName,
    withTactical: false,
    specFiles: [],
    phaseTimeoutMs: 5000,
    spawnCmd: process.execPath,
    spawnArgs: ["-e", hangingParentScript],
  }).catch((err) => {
    runErr = err;
  });

  // 1. Comprova que a porta TCP é realmente ocupada pelo processo descendente antes do timeout
  let portWasOccupied = false;
  const deadline = Date.now() + 45_000;
  while (Date.now() < deadline) {
    if (await checkPortResponding(testPort)) {
      portWasOccupied = true;
      break;
    }
    if (runErr) break;
    await new Promise((r) => setTimeout(r, 100));
  }
  assert.equal(portWasOccupied, true, `esperava que o processo descendente estivesse ouvindo na porta ${testPort}`);

  // Aguarda a conclusão do teardown acionado pelo timeout
  await runPromise;
  assert.ok(runErr, "esperava erro de timeout da fase");
  assert.match(runErr.message, /phase timed out after 5s|phase failed/i);

  // Lê informações gravadas pelo processo descendente
  let parentPid = null;
  let grandchildPid = null;
  if (fs.existsSync(infoFile)) {
    try {
      const data = JSON.parse(fs.readFileSync(infoFile, "utf8"));
      parentPid = data.parentPid;
      grandchildPid = data.grandchildPid;
      fs.unlinkSync(infoFile);
    } catch {
      // ignore
    }
  }

  // Aguarda 1s para o SO finalizar a liberação de recursos
  await new Promise((r) => setTimeout(r, 1000));

  // 2. Comprova que tanto o processo pai quanto o descendente foram terminados
  if (parentPid) {
    assert.equal(isPidAlive(parentPid), false, `processo pai (pid ${parentPid}) permaneceu vivo`);
  }
  if (grandchildPid) {
    assert.equal(isPidAlive(grandchildPid), false, `processo descendente (pid ${grandchildPid}) permaneceu vivo`);
  }

  // 3. Comprova que a porta TCP foi liberada tentando realizar um novo bind imediato
  const canBindAgain = await new Promise((resolve) => {
    const s = net.createServer();
    s.once("error", () => resolve(false));
    s.once("listening", () => {
      s.close(() => resolve(true));
    });
    s.listen(testPort, "127.0.0.1");
  });
  assert.equal(canBindAgain, true, `esperava conseguir realizar novo bind na porta ${testPort} após o teardown`);

  // 4. Verifica que apenas os diretórios temporários pertencentes a esta execução foram removidos
  const tempFiles = fs.readdirSync(tmpDir);
  const leakedDirs = tempFiles.filter((f) => f.startsWith(`wacalls-e2e-state-${phaseName}-`));
  assert.equal(leakedDirs.length, 0, `diretórios residuais vazados desta execução: ${leakedDirs.join(", ")}`);
});
