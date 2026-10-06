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
    let mut sock = tokio::net::TcpStream::connect(("127.0.0.1", local_port))
        .await
        .unwrap();

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
        proxy_type: "http".into(),
        host: "127.0.0.1".into(),
        port: 1,
        username: None,
        password: None,
    };
    let (bridge, local_port) = Socks5Bridge::start(upstream).await.unwrap();
    let mut sock = tokio::net::TcpStream::connect(("127.0.0.1", local_port))
        .await
        .unwrap();
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
        proxy_type: "http".into(),
        host: "127.0.0.1".into(),
        port: 1,
        username: None,
        password: None,
    };
    let (bridge, local_port) = Socks5Bridge::start(upstream).await.unwrap();
    let mut sock = tokio::net::TcpStream::connect(("127.0.0.1", local_port))
        .await
        .unwrap();
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

#[tokio::test]
async fn bridge_authenticates_upstream_socks_and_preserves_tunnel_bytes() {
    let upstream_listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let upstream_port = upstream_listener.local_addr().unwrap().port();
    let upstream_task = tokio::spawn(async move {
        let (mut socket, _) = upstream_listener.accept().await.unwrap();
        let mut greeting = [0; 3];
        socket.read_exact(&mut greeting).await.unwrap();
        assert_eq!(greeting, [5, 1, 2]);
        socket.write_all(&[5, 2]).await.unwrap();
        assert_eq!(socket.read_u8().await.unwrap(), 1);
        let mut username = vec![0; socket.read_u8().await.unwrap() as usize];
        socket.read_exact(&mut username).await.unwrap();
        let mut password = vec![0; socket.read_u8().await.unwrap() as usize];
        socket.read_exact(&mut password).await.unwrap();
        assert_eq!(username, b"private-user");
        assert_eq!(password, b"private-password");
        socket.write_all(&[1, 0]).await.unwrap();
        let mut request = [0; 5];
        socket.read_exact(&mut request).await.unwrap();
        assert_eq!(&request[..4], &[5, 1, 0, 3]);
        let mut destination = vec![0; request[4] as usize];
        socket.read_exact(&mut destination).await.unwrap();
        assert_eq!(destination, b"example.com");
        assert_eq!(socket.read_u16().await.unwrap(), 443);
        // A domain-form bound address must not consume tunneled application data.
        socket
            .write_all(&[5, 0, 0, 3, 3, b'b', b'n', b'd', 0, 80])
            .await
            .unwrap();
        socket.write_all(b"hello").await.unwrap();
    });
    let (bridge, port) = Socks5Bridge::start(ProxyConfig {
        proxy_type: "socks5".into(),
        host: "127.0.0.1".into(),
        port: upstream_port,
        username: Some("private-user".into()),
        password: Some("private-password".into()),
    })
    .await
    .unwrap();
    let mut client = tokio::net::TcpStream::connect(("127.0.0.1", port))
        .await
        .unwrap();
    client.write_all(&[5, 1, 0]).await.unwrap();
    let mut greeting = [0; 2];
    client.read_exact(&mut greeting).await.unwrap();
    assert_eq!(greeting, [5, 0]);
    client.write_all(&[5, 1, 0, 3, 11]).await.unwrap();
    client.write_all(b"example.com").await.unwrap();
    client.write_all(&443u16.to_be_bytes()).await.unwrap();
    let mut response = [0; 15];
    tokio::time::timeout(
        std::time::Duration::from_secs(3),
        client.read_exact(&mut response),
    )
    .await
    .unwrap()
    .unwrap();
    assert_eq!(&response[..2], &[5, 0]);
    assert_eq!(&response[10..], b"hello");
    upstream_task.await.unwrap();
    bridge.stop().await.unwrap();
}

#[tokio::test]
async fn bridge_reports_upstream_auth_failure_without_opening_a_tunnel() {
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let port = listener.local_addr().unwrap().port();
    let server = tokio::spawn(async move {
        let (mut socket, _) = listener.accept().await.unwrap();
        let mut greeting = [0; 3];
        socket.read_exact(&mut greeting).await.unwrap();
        assert_eq!(greeting, [5, 1, 2]);
        socket.write_all(&[5, 0xff]).await.unwrap();
    });
    let (bridge, local_port) = Socks5Bridge::start(ProxyConfig {
        proxy_type: "socks5".into(),
        host: "127.0.0.1".into(),
        port,
        username: Some("user".into()),
        password: Some("wrong".into()),
    })
    .await
    .unwrap();
    let mut client = tokio::net::TcpStream::connect(("127.0.0.1", local_port))
        .await
        .unwrap();
    client.write_all(&[5, 1, 0]).await.unwrap();
    let mut greeting = [0; 2];
    client.read_exact(&mut greeting).await.unwrap();
    client
        .write_all(&[5, 1, 0, 1, 127, 0, 0, 1, 0, 80])
        .await
        .unwrap();
    let mut response = [0; 10];
    tokio::time::timeout(
        std::time::Duration::from_secs(3),
        client.read_exact(&mut response),
    )
    .await
    .unwrap()
    .unwrap();
    assert_eq!(response[1], 5);
    server.await.unwrap();
    bridge.stop().await.unwrap();
}
