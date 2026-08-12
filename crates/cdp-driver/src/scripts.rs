use multizen_core::FingerprintConfig;

pub fn build_webrtc_block_script() -> &'static str {
    r#"(() => { try { const orig = window.RTCPeerConnection; window.RTCPeerConnection = function() { throw new Error("WebRTC disabled"); }; } catch(e) {} })();"#
}

pub fn build_webrtc_spoof_script(spoof_ip: &str) -> String {
    format!(
        r#"(() => {{
  const spoofIp = "{ip}";
  const orig = window.RTCPeerConnection;
  function PatchedRTC(cfg, constraints) {{
    const pc = new orig(cfg, constraints);
    const origAddIce = pc.addIceCandidate.bind(pc);
    pc.addIceCandidate = function(cand, ...rest) {{
      if (cand && cand.candidate) cand.candidate = cand.candidate.replace(/(\d+\.\d+\.\d+\.\d+)/g, spoofIp);
      return origAddIce(cand, ...rest);
    }};
    return pc;
  }}
  PatchedRTC.prototype = orig.prototype;
  window.RTCPeerConnection = PatchedRTC;
  window.webkitRTCPeerConnection = PatchedRTC;
}})();"#,
        ip = spoof_ip
    )
}

pub fn build_fingerprint_preload_script(fp: &FingerprintConfig) -> String {
    format!(
        r#"(() => {{
  const def = (obj, prop, val) => Object.defineProperty(obj, prop, {{get: () => val, configurable: true}});
  try {{ def(navigator, "platform", {platform:?}); }} catch(e) {{}}
  try {{ def(navigator, "hardwareConcurrency", {hc}); }} catch(e) {{}}
  try {{ def(navigator, "deviceMemory", {dm}); }} catch(e) {{}}
  try {{ def(screen, "width", {sw}); def(screen, "height", {sh}); }} catch(e) {{}}
  try {{ def(window, "devicePixelRatio", {dpr}); }} catch(e) {{}}
  try {{
    const origGet = WebGLRenderingContext.prototype.getParameter;
    WebGLRenderingContext.prototype.getParameter = function(p) {{
      if (p === 0x9245 || p === "UNMASKED_VENDOR_WEBGL") return {wv:?};
      if (p === 0x9246 || p === "UNMASKED_RENDERER_WEBGL") return {wr:?};
      return origGet.call(this, p);
    }};
  }} catch(e) {{}}
}})();"#,
        platform = fp.platform,
        hc = fp.hardware_concurrency,
        dm = fp.device_memory,
        sw = fp.screen.width,
        sh = fp.screen.height,
        dpr = fp.dpr,
        wv = fp.webgl.vendor,
        wr = fp.webgl.renderer,
    )
}
