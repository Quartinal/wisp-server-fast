use std::sync::Arc;

use futures::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use wisp_mux::packet::{CloseReason, ConnectPacket};
use wisp_mux::ws::TransportWrite;
use wisp_mux::{ServerMux, WispV2Handshake};

use crate::ws_adapter::{upgrade_and_split, SplitTx};

#[derive(Debug, Clone)]
pub struct ServerConfig {
    pub buffer_size:     u32,
    pub wisp_v2:         bool,
    pub max_connections: Option<u32>,
    pub allowed_hosts:   Vec<String>,
    pub blocked_ports:   Vec<u16>,
}

pub async fn handle_wisp_connection(
    stream: TcpStream,
    head:   Vec<u8>,
    config: Arc<ServerConfig>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let (rx, tx) = upgrade_and_split(stream, head).await?;

    let wisp_v2 = if config.wisp_v2 {
        Some(WispV2Handshake::new(vec![]))
    } else {
        None
    };

    let mux_result = ServerMux::new(rx, tx, config.buffer_size, wisp_v2).await?;
    let (mux, mux_fut) = mux_result.with_no_required_extensions();

    let mux_driver = tokio::spawn(async move {
        if let Err(e) = mux_fut.await {
            eprintln!("[epoxy-server] mux driver: {e}");
        }
    });

    loop {

        let item: Option<(ConnectPacket, wisp_mux::stream::MuxStream<SplitTx>)> =
            mux.wait_for_stream().await;

        let Some((connect, mux_stream)) = item else { break };

        if let Err(reason) = validate_connect(&connect, &config) {
            eprintln!(
                "[epoxy-server] blocked {}:{} — {reason}",
                connect.host, connect.port
            );
            mux_stream.close(CloseReason::ServerStreamUnreachable).await.ok();
            continue;
        }

        let config = Arc::clone(&config);
        tokio::spawn(async move {
            if let Err(e) = proxy_stream(connect, mux_stream, config).await {
                eprintln!("[epoxy-server] proxy: {e}");
            }
        });
    }

    mux_driver.abort();
    Ok(())
}

fn validate_connect(packet: &ConnectPacket, config: &ServerConfig) -> Result<(), &'static str> {
    if config.blocked_ports.contains(&packet.port) {
        return Err("port blocked");
    }
    if !config.allowed_hosts.is_empty()
        && !config.allowed_hosts.iter().any(|h| h == &packet.host)
    {
        return Err("host not in allowlist");
    }
    Ok(())
}

async fn proxy_stream<W: TransportWrite>(
    connect:    ConnectPacket,
    mux_stream: wisp_mux::stream::MuxStream<W>,
    _config:    Arc<ServerConfig>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let addr = format!("{}:{}", connect.host, connect.port);

    let tcp = TcpStream::connect(&addr)
        .await
        .map_err(|e| format!("TCP connect to {addr}: {e}"))?;

    let (tcp_read, tcp_write) = tcp.into_split();

    let (mut wisp_read, mut wisp_write) = mux_stream.into_async_rw().into_split();

    use tokio_util::compat::{TokioAsyncReadCompatExt, TokioAsyncWriteCompatExt};
    let mut tcp_read  = tcp_read.compat();
    let mut tcp_write = tcp_write.compat_write();

    let c2s = tokio::spawn(async move {
        let mut buf = vec![0u8; 65536];
        loop {
            let n = wisp_read.read(&mut buf).await?;
            if n == 0 { break; }
            tcp_write.write_all(&buf[..n]).await?;
        }
        tcp_write.close().await?;
        Ok::<_, std::io::Error>(())
    });

    let s2c = tokio::spawn(async move {
        let mut buf = vec![0u8; 65536];
        loop {
            let n = tcp_read.read(&mut buf).await?;
            if n == 0 { break; }
            wisp_write.write_all(&buf[..n]).await?;
        }
        wisp_write.close().await?;
        Ok::<_, std::io::Error>(())
    });

    let (r1, r2) = tokio::join!(c2s, s2c);
    if let Ok(Err(e)) = r1 { eprintln!("[epoxy-server] c2s: {e}"); }
    if let Ok(Err(e)) = r2 { eprintln!("[epoxy-server] s2c: {e}"); }

    Ok(())
}