"use strict";

const $ = (id) => document.getElementById(id);

/** 带协议错误码与耗时的调用异常。 */
class BridgeError extends Error {
  constructor(code, message, elapsed) {
    super(message);
    this.name = "BridgeError";
    this.code = code;
    this.elapsed = elapsed;
  }
}

function log(message, kind = "info") {
  const line = document.createElement("div");
  line.className = `log-line ${kind}`;
  const time = new Date().toLocaleTimeString("zh-CN", { hour12: false });
  line.textContent = `[${time}] ${message}`;
  const box = $("log");
  box.append(line);
  box.scrollTop = box.scrollHeight;
}

/** 统一调用 Go 后端方法。 */
async function call(method, params = {}) {
  const started = performance.now();
  let response;
  try {
    response = await fetch("/api/call", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ method, params }),
    });
  } catch (cause) {
    throw new BridgeError("network_error", `无法连接 Go 后端：${cause.message}`, 0);
  }
  const elapsed = Math.round(performance.now() - started);
  let payload;
  try {
    payload = await response.json();
  } catch {
    throw new BridgeError("invalid_response", `HTTP ${response.status}，响应不是 JSON`, elapsed);
  }
  if (!payload.ok) {
    const code = payload.error?.code ?? "unknown";
    const message = payload.error?.message ?? `调用失败（HTTP ${response.status}）`;
    throw new BridgeError(code, message, elapsed);
  }
  log(`${method} → ok (${elapsed} ms)`, "ok");
  return payload.data;
}

function setStatus(state, text) {
  const status = $("status");
  status.dataset.state = state;
  $("status-text").textContent = text;
}

function formatTime(value) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN", { hour12: false });
}

function formatUptime(seconds) {
  const total = Math.max(0, Math.floor(seconds));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h} 小时 ${m} 分`;
  if (m > 0) return `${m} 分 ${s} 秒`;
  return `${s} 秒`;
}

function renderInfo(info) {
  const values = [
    info.name,
    info.version,
    info.goVersion,
    `${info.os}/${info.arch}`,
    info.pid,
    formatUptime(info.uptimeSeconds),
    formatTime(info.startedAt),
  ];
  document.querySelectorAll("#info dd").forEach((dd, index) => {
    dd.textContent = values[index] ?? "—";
  });
}

async function refreshInfo() {
  const info = await call("app.info");
  renderInfo(info);
}

function renderCounter(state) {
  $("counter-value").textContent = state.value;
}

async function refreshCounter() {
  renderCounter(await call("counter.get"));
}

async function adjustCounter(delta) {
  renderCounter(await call("counter.add", { delta }));
}

async function resetCounter() {
  renderCounter(await call("counter.reset"));
}

async function greet(name) {
  const greeting = await call("greeter.greet", { name });
  $("greeting").textContent = `${greeting.message}（${formatTime(greeting.at)}）`;
}

function reportError(action, error) {
  if (error instanceof BridgeError) {
    log(`${action} → ${error.code}: ${error.message} (${error.elapsed} ms)`, "error");
  } else {
    log(`${action} → 未预期错误: ${error.message}`, "error");
  }
}

function bindEvents() {
  $("refresh").addEventListener("click", async () => {
    try {
      await refreshInfo();
    } catch (error) {
      reportError("app.info", error);
    }
  });

  $("greet-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const name = $("name").value;
    try {
      await greet(name);
    } catch (error) {
      $("greeting").textContent = "调用失败，详见下方日志。";
      reportError("greeter.greet", error);
    }
  });

  document.querySelectorAll("[data-delta]").forEach((button) => {
    button.addEventListener("click", async () => {
      const delta = Number(button.dataset.delta);
      try {
        await adjustCounter(delta);
      } catch (error) {
        reportError(`counter.add(${delta})`, error);
      }
    });
  });

  $("reset").addEventListener("click", async () => {
    try {
      await resetCounter();
    } catch (error) {
      reportError("counter.reset", error);
    }
  });

  $("clear").addEventListener("click", () => {
    $("log").replaceChildren();
  });
}

async function bootstrap() {
  bindEvents();
  log("页面已加载，开始探测 Go 后端…");
  try {
    const health = await fetch("/api/health", { headers: { Accept: "application/json" } });
    const payload = await health.json();
    if (!payload.ok) throw new Error(payload.error?.message ?? "健康检查失败");
    setStatus("ok", "Go 后端已连接");
    log(`健康检查通过，已注册方法：${payload.data.methods.join(", ")}`, "ok");
    await Promise.all([refreshInfo(), refreshCounter()]);
    $("name").focus();
  } catch (error) {
    setStatus("error", "后端不可用");
    reportError("bootstrap", error);
  }
}

bootstrap();
