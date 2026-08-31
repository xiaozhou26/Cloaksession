use browser_launcher::version::{parse_version_output, synchronize_managed_fingerprint_version};
use multizen_core::FingerprintConfig;

fn fingerprint(user_agent: &str, sec_ch_ua: &str, full_version_list: &str) -> FingerprintConfig {
    let mut fp = profile_manager::fingerprint::default_fingerprint("test");
    fp.user_agent = user_agent.into();
    fp.client_hints.sec_ch_ua = sec_ch_ua.into();
    fp.client_hints.sec_ch_ua_full_version_list = full_version_list.into();
    fp
}

#[test]
fn synchronizes_generated_chrome_version_without_starting_browser() {
    let mut fp = fingerprint(
        "Mozilla/5.0 Chrome/148.0.0.0 Safari/537.36",
        r#""Chromium";v="148", "Google Chrome";v="148", "Not?A_Brand";v="99""#,
        r#""Chromium";v="148.0.0.0", "Google Chrome";v="148.0.0.0", "Not?A_Brand";v="99.0.0.0""#,
    );

    assert!(synchronize_managed_fingerprint_version(&mut fp, "151.0.7922.108"));
    assert!(fp.user_agent.contains("Chrome/151.0.0.0"));
    assert!(fp.client_hints.sec_ch_ua.contains("Chromium\";v=\"151\""));
    assert!(fp.client_hints.sec_ch_ua_full_version_list.contains("151.0.0.0"));
}

#[test]
fn resynchronizes_a_previously_managed_chrome_version() {
    let mut fp = fingerprint(
        "Mozilla/5.0 Chrome/151.0.0.0 Safari/537.36",
        r#""Chromium";v="151", "Google Chrome";v="151", "Not?A_Brand";v="99""#,
        r#""Chromium";v="151.0.0.0", "Google Chrome";v="151.0.0.0", "Not?A_Brand";v="99.0.0.0""#,
    );

    assert!(synchronize_managed_fingerprint_version(&mut fp, "152.0.7977.65"));
    assert!(fp.user_agent.contains("Chrome/152.0.0.0"));
    assert!(fp.client_hints.sec_ch_ua.contains("Chromium\";v=\"152\""));
    assert!(fp.client_hints.sec_ch_ua_full_version_list.contains("152.0.0.0"));
}

#[test]
fn preserves_explicit_standard_chrome_user_agent_when_hints_do_not_match() {
    let mut fp = fingerprint(
        "Mozilla/5.0 Chrome/120.0.0.0 Safari/537.36",
        r#""Chromium";v="151", "Google Chrome";v="151", "Not?A_Brand";v="99""#,
        r#""Chromium";v="151.0.0.0", "Google Chrome";v="151.0.0.0", "Not?A_Brand";v="99.0.0.0""#,
    );

    assert!(!synchronize_managed_fingerprint_version(&mut fp, "152.0.7977.65"));
    assert!(fp.user_agent.contains("Chrome/120.0.0.0"));
}

#[test]
fn preserves_explicit_custom_user_agent() {
    let mut fp = fingerprint(
        "Custom/99.1 test-agent",
        "custom hints",
        "custom full hints",
    );

    assert!(!synchronize_managed_fingerprint_version(&mut fp, "151.0.7922.108"));
    assert_eq!(fp.user_agent, "Custom/99.1 test-agent");
    assert_eq!(fp.client_hints.sec_ch_ua, "custom hints");
}

#[test]
fn parses_chrome_version_line() {
    assert_eq!(
        parse_version_output("Google Chrome 148.0.0.0 unknown"),
        Some("148.0.0.0".to_string())
    );
}

#[test]
fn parses_cft_version_line() {
    assert_eq!(
        parse_version_output("Google Chrome for Testing 145.0.6123.5"),
        Some("145.0.6123.5".to_string())
    );
}

#[test]
fn returns_none_on_garbage() {
    assert_eq!(parse_version_output("not a version"), None);
}
