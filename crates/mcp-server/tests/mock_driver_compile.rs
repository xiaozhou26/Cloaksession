//! Compile-only guard so `cargo check --tests` exercises the mock driver.
//! Real tool tests (Task 6) will `mod mock_driver;` this file themselves.

mod mock_driver;
use mcp_server::driver::BrowserDriver;

#[test]
fn mock_driver_constructs() {
    let m = mock_driver::MockBrowserDriver::new();
    assert!(!m.is_running("never-launched"));
}
