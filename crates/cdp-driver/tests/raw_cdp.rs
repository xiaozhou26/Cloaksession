//! CDP session integration tests.

#[test]
fn session_module_exposes_raw_cdp_entrypoint() {
    let _method = cdp_driver::session::BrowserSession::cdp_send;
}
