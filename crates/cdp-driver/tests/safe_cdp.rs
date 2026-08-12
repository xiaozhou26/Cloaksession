use cdp_driver::safe_cdp::{cloak_allows_domain, SafeEnableRefcount};
use multizen_core::BrowserEngine;

#[test]
fn refcount_first_enable_returns_true() {
    let r = SafeEnableRefcount::new();
    assert!(r.should_enable("Runtime"));
    r.enable("Runtime");
    assert_eq!(r.count("Runtime"), 1);
}

#[test]
fn refcount_second_enable_returns_false() {
    let r = SafeEnableRefcount::new();
    r.enable("Runtime");
    assert!(!r.should_enable("Runtime"), "already enabled → no-op");
}

#[test]
fn refcount_disable_only_when_reaches_zero() {
    let r = SafeEnableRefcount::new();
    r.enable("Runtime");
    r.enable("Runtime");
    // count = 2, disable brings to 1 → should_disable false
    assert!(!r.should_disable("Runtime"));
    r.disable("Runtime");
    // now count = 1, one more disable → 0 → should_disable true
    assert!(r.should_disable("Runtime"));
    r.disable("Runtime");
    assert_eq!(r.count("Runtime"), 0);
}

#[test]
fn cloak_rejects_risky_domains() {
    assert!(!cloak_allows_domain("Runtime", BrowserEngine::Cloakbrowser));
    assert!(!cloak_allows_domain("Network", BrowserEngine::Cloakbrowser));
    assert!(cloak_allows_domain("DOM", BrowserEngine::Cloakbrowser));
    assert!(cloak_allows_domain("Page", BrowserEngine::Cloakbrowser));
}

#[test]
fn cft_allows_all() {
    assert!(cloak_allows_domain("Runtime", BrowserEngine::Cft));
    assert!(cloak_allows_domain("Network", BrowserEngine::Cft));
}

#[test]
fn cloak_allows_on_cft_engine() {
    // CFT engine ignores cloak restrictions
    assert!(cloak_allows_domain("Runtime", BrowserEngine::Cft));
}
