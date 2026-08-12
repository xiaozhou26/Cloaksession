#[allow(unused_imports)]
use browser_launcher::proxy_geo::{parse_ipapi_response, ProxyGeoResult};

#[test]
fn parse_valid_response() {
    let body = r#"{"country_code":"US","country_name":"United States","timezone":"America/New_York","city":"New York","ip":"1.2.3.4","latitude":40.7,"longitude":-74.0}"#;
    let r = parse_ipapi_response(body).unwrap();
    assert_eq!(r.country, "us"); // lowercased
    assert_eq!(r.country_name, "United States");
    assert_eq!(r.timezone, "America/New_York");
    assert_eq!(r.ip, "1.2.3.4");
    assert_eq!(r.latitude, Some(40.7));
    assert_eq!(r.longitude, Some(-74.0));
}

#[test]
fn parse_rejects_missing_country() {
    let body = r#"{"timezone":"America/New_York"}"#;
    assert!(parse_ipapi_response(body).is_err());
}

#[test]
fn parse_rejects_missing_timezone() {
    let body = r#"{"country_code":"US"}"#;
    assert!(parse_ipapi_response(body).is_err());
}

#[test]
fn parse_handles_error_field() {
    let body = r#"{"error":"rate limited","reason":"too many requests"}"#;
    assert!(parse_ipapi_response(body).is_err());
}

#[test]
fn parse_drops_non_number_lat_lon() {
    let body = r#"{"country_code":"US","country_name":"US","timezone":"America/New_York","city":"x","ip":"1.1.1.1","latitude":"not a number"}"#;
    let r = parse_ipapi_response(body).unwrap();
    assert_eq!(r.latitude, None);
    assert_eq!(r.longitude, None);
}
