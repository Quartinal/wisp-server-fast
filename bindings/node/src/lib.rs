#![deny(clippy::all)]

use std::sync::Arc;
use std::sync::atomic::{AtomicU32, Ordering};

use napi::bindgen_prelude::*;
use napi_derive::napi;

mod connection;
mod ws_adapter;

use connection::handle_wisp_connection;

#[napi(object)]
#[derive(Debug, Clone)]
pub struct EpoxyServerConfig {
    pub buffer_size:     Option<u32>,
    pub wisp_v2:         Option<bool>,
    pub max_connections: Option<u32>,
    pub allowed_hosts:   Option<Vec<String>>,
    pub blocked_ports:   Option<Vec<u16>>,
}

#[napi]
pub struct EpoxyServer {
    config:           Arc<connection::ServerConfig>,
    connection_count: Arc<AtomicU32>,
}

#[napi]
impl EpoxyServer {
    #[napi(constructor)]
    pub fn new(config: Option<EpoxyServerConfig>) -> napi::Result<Self> {
        let c = config.unwrap_or(EpoxyServerConfig {
            buffer_size: None, wisp_v2: None, max_connections: None,
            allowed_hosts: None, blocked_ports: None,
        });
        Ok(Self {
            config: Arc::new(connection::ServerConfig {
                buffer_size:     c.buffer_size.unwrap_or(128),
                wisp_v2:         c.wisp_v2.unwrap_or(true),
                max_connections: c.max_connections,
                allowed_hosts:   c.allowed_hosts.unwrap_or_default(),
                blocked_ports:   c.blocked_ports.unwrap_or_default(),
            }),
            connection_count: Arc::new(AtomicU32::new(0)),
        })
    }

    #[napi]
    pub async fn route_connection(&self, socket_fd: i64, head: Buffer) -> napi::Result<()> {
        if let Some(max) = self.config.max_connections {
            if self.connection_count.load(Ordering::Relaxed) >= max {
                return Err(napi::Error::from_reason(format!("max_connections ({max}) exceeded")));
            }
        }

        let config  = Arc::clone(&self.config);
        let counter = Arc::clone(&self.connection_count);
        let head    = head.to_vec();
        let fd      = socket_fd as i32;

        let stream = unsafe {
            #[cfg(unix)]
            {
                use std::os::unix::io::FromRawFd;
                let s = std::net::TcpStream::from_raw_fd(fd);
                s.set_nonblocking(true).map_err(|e| napi::Error::from_reason(e.to_string()))?;
                tokio::net::TcpStream::from_std(s).map_err(|e| napi::Error::from_reason(e.to_string()))?
            }
            #[cfg(windows)]
            {
                use std::os::windows::io::FromRawSocket;
                let s = std::net::TcpStream::from_raw_socket(fd as u64);
                s.set_nonblocking(true).map_err(|e| napi::Error::from_reason(e.to_string()))?;
                tokio::net::TcpStream::from_std(s).map_err(|e| napi::Error::from_reason(e.to_string()))?
            }
        };

        counter.fetch_add(1, Ordering::Relaxed);
        tokio::spawn(async move {
            if let Err(e) = handle_wisp_connection(stream, head, config).await {
                eprintln!("[epoxy-server-node] {e}");
            }
            counter.fetch_sub(1, Ordering::Relaxed);
        });

        Ok(())
    }

    #[napi(getter)]
    pub fn connection_count(&self) -> u32 {
        self.connection_count.load(Ordering::Relaxed)
    }
}