use browser_launcher::version::parse_version_output;

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
