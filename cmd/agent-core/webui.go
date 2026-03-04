package main

import "net/http"

const webUIPage = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Agent Core Web UI</title>
  <style>
    :root {
      --bg-1: #f6f3eb;
      --bg-2: #e8f1f6;
      --surface: #ffffff;
      --surface-2: #f4f6f8;
      --border: #d5dde4;
      --text: #1c2733;
      --muted: #5a6978;
      --accent: #0b6fae;
      --accent-strong: #045887;
      --ok: #1d7d46;
      --warn: #9a6200;
      --err: #b42318;
      --radius: 16px;
      --shadow: 0 16px 40px rgba(18, 40, 61, 0.12);
    }

    * { box-sizing: border-box; }

    body {
      margin: 0;
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 24px;
      font-family: "IBM Plex Sans", "Segoe UI", sans-serif;
      color: var(--text);
      background:
        radial-gradient(circle at 20% 10%, #d9e6ef 0%, transparent 42%),
        radial-gradient(circle at 90% 90%, #efe6d6 0%, transparent 48%),
        linear-gradient(145deg, var(--bg-1), var(--bg-2));
    }

    .app {
      width: min(1200px, 100%);
      display: grid;
      gap: 12px;
      background: color-mix(in srgb, var(--surface) 84%, transparent);
      border: 1px solid var(--border);
      border-radius: calc(var(--radius) + 6px);
      padding: 18px;
      box-shadow: var(--shadow);
      backdrop-filter: blur(3px);
    }

    .chat {
      min-height: 240px;
      max-height: 56vh;
      overflow: auto;
      border: 1px solid var(--border);
      background: var(--surface-2);
      border-radius: var(--radius);
      padding: 14px;
      display: flex;
      flex-direction: column;
      gap: 10px;
    }

    .empty {
      color: var(--muted);
      text-align: center;
      align-self: center;
      font-size: 14px;
    }

    .message {
      max-width: 92%;
      width: auto;
      height: auto;
      display: inline-block;
      padding: 10px 12px;
      border-radius: 12px;
      white-space: pre-wrap;
      overflow-wrap: break-word;
      word-break: break-word;
      line-height: 1.42;
      font-size: 15px;
    }

    .message.user {
      align-self: flex-end;
      background: #d8ebf7;
      border: 1px solid #b7d9ef;
    }

    .message.agent {
      align-self: flex-start;
      background: #ffffff;
      border: 1px solid #d8dfe5;
    }

    .composer {
      display: grid;
      gap: 10px;
    }

    .compose-row {
      display: grid;
      grid-template-columns: 1fr auto;
      gap: 10px;
      align-items: center;
    }

    textarea {
      width: 100%;
      min-height: 48px;
      max-height: 240px;
      resize: none;
      overflow-y: hidden;
      border-radius: 12px;
      border: 1px solid var(--border);
      padding: 12px;
      color: var(--text);
      background: #fff;
      font: 15px/1.4 "IBM Plex Sans", "Segoe UI", sans-serif;
      outline: none;
    }

    textarea:focus {
      border-color: var(--accent);
      box-shadow: 0 0 0 3px rgba(11, 111, 174, 0.14);
    }

    button {
      height: 44px;
      min-width: 124px;
      border: 0;
      border-radius: 12px;
      padding: 0 16px;
      font-weight: 600;
      font-size: 14px;
      cursor: pointer;
      color: #fff;
      background: linear-gradient(180deg, var(--accent), var(--accent-strong));
      transition: transform 120ms ease, filter 120ms ease;
    }

    button:hover { filter: brightness(1.06); }
    button:active { transform: translateY(1px); }
    button:disabled { cursor: not-allowed; opacity: 0.6; }

    .status {
      border: 1px solid var(--border);
      border-radius: 12px;
      padding: 10px 12px;
      background: #fff;
      font-size: 13px;
      color: var(--muted);
      display: grid;
      gap: 8px;
    }

    .status-head {
      display: flex;
      align-items: baseline;
      flex-wrap: wrap;
      column-gap: 8px;
      row-gap: 4px;
    }

    .status strong {
      color: var(--text);
      font-size: 12px;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      margin-right: 0;
    }

    .status-extra {
      display: grid;
      gap: 8px;
      padding-top: 8px;
      border-top: 1px dashed var(--border);
    }

    .status-extra[hidden] {
      display: none;
    }

    .status-section-title {
      color: var(--text);
      font-size: 12px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.02em;
      margin-bottom: 4px;
    }

    .status-list {
      margin: 0;
      padding-left: 18px;
      color: var(--text);
      display: grid;
      gap: 3px;
    }

    .status-list li {
      line-height: 1.36;
    }

    .status.running strong { color: var(--warn); }
    .status.done strong { color: var(--ok); }
    .status.error strong { color: var(--err); }

    @media (max-width: 640px) {
      body { padding: 12px; }
      .app { padding: 12px; }
      .compose-row {
        grid-template-columns: 1fr;
      }
      button {
        width: 100%;
      }
      .chat { min-height: 200px; }
    }
  </style>
</head>
<body>
  <main class="app">
    <section id="chat" class="chat">
      <div class="empty" id="empty-chat">История чата хранится только до перезагрузки страницы.</div>
    </section>

    <form id="run-form" class="composer">
      <div class="compose-row">
        <textarea id="prompt" placeholder="Введите запрос к AI агенту..." autocomplete="off"></textarea>
        <button type="submit" id="send-btn">Отправить</button>
      </div>
    </form>

    <div id="status" class="status idle">
      <div class="status-head">
        <strong id="status-title">Статус</strong>
        <span id="status-details">готов к запросу</span>
      </div>
      <div id="status-extra" class="status-extra" hidden></div>
    </div>
  </main>

  <script>
    const chat = document.getElementById("chat");
    const emptyChat = document.getElementById("empty-chat");
    const form = document.getElementById("run-form");
    const promptEl = document.getElementById("prompt");
    const sendBtn = document.getElementById("send-btn");
    const statusEl = document.getElementById("status");
    const statusTitleEl = document.getElementById("status-title");
    const statusDetailsEl = document.getElementById("status-details");
    const statusExtraEl = document.getElementById("status-extra");

    const clearNode = (node) => {
      while (node.firstChild) {
        node.removeChild(node.firstChild);
      }
    };

    const renderStatusSection = (title, values) => {
      const section = document.createElement("section");
      const sectionTitle = document.createElement("div");
      sectionTitle.className = "status-section-title";
      sectionTitle.textContent = title;
      section.appendChild(sectionTitle);

      const list = document.createElement("ul");
      list.className = "status-list";
      if (!Array.isArray(values) || values.length === 0) {
        const item = document.createElement("li");
        item.textContent = "(none)";
        list.appendChild(item);
      } else {
        for (const value of values) {
          const item = document.createElement("li");
          item.textContent = value;
          list.appendChild(item);
        }
      }
      section.appendChild(list);
      statusExtraEl.appendChild(section);
    };

    const formatPlanningStep = (step, index) => {
      const stepNumber = step && Number.isInteger(step.step) ? step.step : (index + 1);
      const actionType = step && typeof step.action_type === "string" && step.action_type ? step.action_type : "-";
      const toolName = step && typeof step.tool_name === "string" ? step.tool_name : "";
      const doneFlag = step && typeof step.done === "boolean" ? (step.done ? "done=true" : "done=false") : "";
      const reasoning = step && typeof step.reasoning_summary === "string" ? step.reasoning_summary : "";
      const expected = step && typeof step.expected_outcome === "string" ? step.expected_outcome : "";

      const parts = ["#" + stepNumber, "action=" + actionType];
      if (toolName) parts.push("tool=" + toolName);
      if (doneFlag) parts.push(doneFlag);
      if (reasoning) parts.push("why=" + reasoning);
      if (expected) parts.push("expect=" + expected);
      return parts.join(", ");
    };

    const renderStatusDetails = (meta) => {
      clearNode(statusExtraEl);
      if (!meta) {
        statusExtraEl.hidden = true;
        return;
      }

      const planningSteps = Array.isArray(meta.planningSteps)
        ? meta.planningSteps.map((step, index) => formatPlanningStep(step, index))
        : [];
      const calledTools = Array.isArray(meta.calledTools) ? meta.calledTools : [];
      const mcpTools = Array.isArray(meta.mcpTools) ? meta.mcpTools : [];
      const skills = Array.isArray(meta.skills) ? meta.skills : [];

      renderStatusSection("Called tools", calledTools);
      renderStatusSection("MCP tools", mcpTools);
      renderStatusSection("Skills", skills);
      renderStatusSection("Planning steps", planningSteps);
      statusExtraEl.hidden = false;
    };

    const setStatus = (state, details, meta = null) => {
      statusEl.className = "status " + state;
      const title = state === "running" ? "Выполняется" : state === "done" ? "Выполнен" : state === "error" ? "Ошибка" : "Статус";
      statusTitleEl.textContent = title;
      statusDetailsEl.textContent = details;
      if (state === "done") {
        renderStatusDetails(meta);
      } else {
        renderStatusDetails(null);
      }
    };

    const addMessage = (role, text) => {
      if (emptyChat) {
        emptyChat.remove();
      }
      const node = document.createElement("div");
      node.className = "message " + role;
      node.textContent = text;
      chat.appendChild(node);
      chat.scrollTop = chat.scrollHeight;
    };

    const promptMinHeight = Number.parseFloat(getComputedStyle(promptEl).minHeight) || 48;
    const promptMaxHeight = Number.parseFloat(getComputedStyle(promptEl).maxHeight) || 240;

    const autoResize = () => {
      promptEl.style.height = "0px";
      const contentHeight = promptEl.scrollHeight;
      const nextHeight = Math.max(promptMinHeight, Math.min(contentHeight, promptMaxHeight));
      promptEl.style.height = String(nextHeight) + "px";
      promptEl.style.overflowY = contentHeight > promptMaxHeight ? "auto" : "hidden";
    };

    promptEl.addEventListener("input", autoResize);
    autoResize();

    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const input = promptEl.value.trim();
      if (!input) {
        setStatus("error", "пустой запрос: введите текст перед отправкой");
        return;
      }

      addMessage("user", input);
      setStatus("running", "POST /v1/agent/run");
      sendBtn.disabled = true;
      promptEl.disabled = true;

      const startedAt = performance.now();
      try {
        const response = await fetch("/v1/agent/run", {
          method: "POST",
          headers: {
            "Content-Type": "application/json"
          },
          body: JSON.stringify({ input })
        });

        const raw = await response.text();
        let payload = null;
        try {
          payload = raw ? JSON.parse(raw) : null;
        } catch (_err) {
          payload = null;
        }

        const elapsedMs = Math.round(performance.now() - startedAt);
        if (!response.ok) {
          const errorText = payload && payload.error ? payload.error : (raw || "unknown error");
          addMessage("agent", "Ошибка: " + errorText);
          setStatus("error", "HTTP " + response.status + ", " + elapsedMs + "ms, message=" + errorText);
          return;
        }

        const finalResponse = payload && typeof payload.final_response === "string" ? payload.final_response : "";
        addMessage("agent", finalResponse || "(пустой ответ)");
        const steps = payload && Number.isInteger(payload.steps) ? payload.steps : "-";
        const toolCalls = payload && Number.isInteger(payload.tool_calls) ? payload.tool_calls : "-";
        const stopReason = payload && payload.stop_reason ? payload.stop_reason : "-";
        const sessionID = payload && payload.session_id ? payload.session_id : "-";
        const correlationID = payload && payload.correlation_id ? payload.correlation_id : "-";
        const planningSteps = payload && Array.isArray(payload.planning_steps) ? payload.planning_steps : [];
        const calledTools = payload && Array.isArray(payload.called_tools) ? payload.called_tools.filter((item) => typeof item === "string") : [];
        const mcpTools = payload && Array.isArray(payload.mcp_tools) ? payload.mcp_tools.filter((item) => typeof item === "string") : [];
        const skills = payload && Array.isArray(payload.skills) ? payload.skills.filter((item) => typeof item === "string") : [];
        setStatus(
          "done",
          "HTTP " + response.status + ", " + elapsedMs + "ms, steps=" + steps + ", tool_calls=" + toolCalls + ", stop=" + stopReason + ", session=" + sessionID + ", correlation=" + correlationID,
          {
            planningSteps,
            calledTools,
            mcpTools,
            skills
          }
        );
      } catch (error) {
        const elapsedMs = Math.round(performance.now() - startedAt);
        const msg = error instanceof Error ? error.message : "network error";
        addMessage("agent", "Ошибка сети: " + msg);
        setStatus("error", String(elapsedMs) + "ms, network=" + msg);
      } finally {
        sendBtn.disabled = false;
        promptEl.disabled = false;
        promptEl.value = "";
        autoResize();
        promptEl.focus();
      }
    });
  </script>
</body>
</html>
`

// handleWebUI отдает одностраничный интерфейс тестирования агента.
func (s *apiServer) handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/ui" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(webUIPage))
}
