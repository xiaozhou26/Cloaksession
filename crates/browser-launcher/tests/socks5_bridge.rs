use browser_launcher::socks5_bridge::Socks5Bridge;
use multizen_core::ProxyConfig;
use tokio::io::{AsyncReadExt, AsyncWriteExt};

#[tokio::test]
async fn bridge_accepts_greeting_and_replies_no_auth() {
    let upstream = ProxyConfig {
        proxy_type: "http".into(),
        host: "127.0.0.1".into(),
        port: 1, // won't actually connect in this test (we stop before CONNECT)
        username: None,
        password: None,
    };
    let (bridge, local_port) = Socks5Bridge::start(upstream).await.unwrap();
    let mut sock = tokio::net::TcpStream::connect(("127.0.0.1", local_port)).await.unwrap();

    // Client greeting: VER=5, NMETHODS=1, METHOD=0 (no-auth)
    sock.write_all(&[0x05, 0x01, 0x00]).await.unwrap();
    let mut reply = [0u8; 2];
    sock.read_exact(&mut reply).await.unwrap();
    assert_eq!(reply, [0x05, 0x00], "server must select no-auth (0x00)");

    bridge.stop().await.unwrap();
}

#[tokio::test]
async fn bridge_rejects_unsupported_command() {
    let upstream = ProxyConfig {
        proxy_type: "http".into(), host: "127.0.0.1".into(), port: 1,
        username: None, password: None,
    };
    let (bridge, local_port) = Socks5Bridge::start(upstream).await.unwrap();
    let mut sock = tokio::net::TcpStream::connect(("127.0.0.1", local_port)).await.unwrap();
    sock.write_all(&[0x05, 0x01, 0x00]).await.unwrap();
    let mut _g = [0u8; 2];
    sock.read_exact(&mut _g).await.unwrap();

    // Request: VER=5, CMD=0x02 (BIND, unsupported), RSV=0, ATYP=0x01, IPv4, port
    let req = [0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0x00, 0x50];
    sock.write_all(&req).await.unwrap();
    let mut reply = [0u8; 2];
    sock.read_exact(&mut reply).await.unwrap();
    assert_eq!(reply[0], 0x05);
    assert_eq!(reply[1], 0x07, "BIND must get command-not-supported (0x07)");

    bridge.stop().await.unwrap();
}

#[tokio::test]
async fn bridge_rejects_unsupported_address_type() {
    let upstream = ProxyConfig {
        proxy_type: "http".into(), host: "127.0.0.1".into(), port: 1,
        username: None, password: None,
    };
    let (bridge, local_port) = Socks5Bridge::start(upstream).await.unwrap();
    let mut sock = tokio::net::TcpStream::connect(("127.0.0.1", local_port)).await.unwrap();
    sock.write_all(&[0x05, 0x01, 0x00]).await.unwrap();
    let mut _g = [0u8; 2];
    sock.read_exact(&mut _g).await.unwrap();

    // ATYP=0x02 (unsupported — we only do 0x01/0x03/0x04)
    let req = [0x05, 0x01, 0x00, 0x02, 0x00, 0x50];
    sock.write_all(&req).await.unwrap();
    let mut reply = [0u8; 2];
    sock.read_exact(&mut reply).await.unwrap();
    assert_eq!(reply[1], 0x08, "unknown ATYP must get 0x08");

    bridge.stop().await.unwrap();
}
