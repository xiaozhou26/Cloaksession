"use strict";

window.reverseFixture = {
  ready: true,
  http: {},
  sockets: {},
  scheduleXHR(token) {
    this.http[token] = { state: "scheduled" };
    // Return before the breakpoint can pause the tool's evaluation request.
    setTimeout(() => reverseSendXHR(token), 250);
    return token;
  },
  scheduleStep() {
    setTimeout(reverseStepProbe, 250);
    return "step-scheduled";
  },
  openSocket(token) {
    const state = this.sockets[token] = { received: null, error: null };
    const socket = new WebSocket(`ws://${location.host}/socket?token=${encodeURIComponent(token)}`);
    socket.addEventListener("open", () => socket.send(`client:${token}`));
    socket.addEventListener("message", event => {
      state.received = event.data;
      socket.close();
    });
    socket.addEventListener("error", () => { state.error = "socket error"; });
    return token;
  }
};

function reverseStepProbe() {
  let progress = 10;
  progress += 7;
  window.reverseFixture.stepResult = progress;
}

function reverseSendXHR(token) {
  const payload = JSON.stringify({ token, message: "reverse-body-你好" });
  const request = new XMLHttpRequest();
  request.open("POST", `/api/echo?token=${encodeURIComponent(token)}`);
  request.setRequestHeader("Content-Type", "application/json");
  request.onload = () => {
    window.reverseFixture.http[token] = {
      state: "done", status: request.status, body: JSON.parse(request.responseText)
    };
  };
  request.onerror = () => { window.reverseFixture.http[token] = { state: "error" }; };
  request.send(payload);
}
