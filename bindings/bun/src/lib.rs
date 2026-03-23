#![deny(clippy::all)]

use std::sync::Arc;
use std::sync::atomic::{AtomicU32, Ordering};

use napi::bindgen_prelude::*;
use napi_derive::napi;
use tokio::net::TcpListener;

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
    port:             u16,
}

#[napi]
impl EpoxyServer {
    #[napi(constructor)]
    pub fn new(config: Option<EpoxyServerConfig>) -> napi::Result<Self> {
        let c = config.unwrap_or(EpoxyServerConfig {
            buffer_size: None, wisp_v2: None, max_connections: None,
            allowed_hosts: None, blocked_ports: None,
        });

        let server_config = Arc::new(connection::ServerConfig {
            buffer_size:     c.buffer_size.unwrap_or(128),
            wisp_v2:         c.wisp_v2.unwrap_or(true),
            max_connections: c.max_connections,
            allowed_hosts:   c.allowed_hosts.unwrap_or_default(),
            blocked_ports:   c.blocked_ports.unwrap_or_default(),
        });

        let connection_count = Arc::new(AtomicU32::new(0));

        let std_listener = std::net::TcpListener::bind("127.0.0.1:0")
            .map_err(|e| napi::Error::from_reason(e.to_string()))?;
        std_listener
            .set_nonblocking(true)
            .map_err(|e| napi::Error::from_reason(e.to_string()))?;
        let port = std_listener.local_addr()
            .map_err(|e| napi::Error::from_reason(e.to_string()))?
            .port();

        let config_clone  = Arc::clone(&server_config);
        let counter_clone = Arc::clone(&connection_count);

        tokio::spawn(async move {
            let listener = TcpListener::from_std(std_listener).expect("TcpListener::from_std");
            loop {
                match listener.accept().await {
                    Ok((stream, _addr)) => {
                        let config  = Arc::clone(&config_clone);
                        let counter = Arc::clone(&counter_clone);

                        if let Some(max) = config.max_connections {
                            if counter.load(Ordering::Relaxed) >= max {
                                drop(stream);
                                continue;
                            }
                        }

                        counter.fetch_add(1, Ordering::Relaxed);
                        tokio::spawn(async move {
                            if let Err(e) = handle_wisp_connection(stream, vec![], config).await {
                                eprintln!("[epoxy-server-bun] {e}");
                            }
                            counter.fetch_sub(1, Ordering::Relaxed);
                        });
                    }
                    Err(e) => {
                        eprintln!("[epoxy-server-bun] accept error: {e}");
                    }
                }
            }
        });

        Ok(Self { config: server_config, connection_count, port })
    }

    #[napi(getter)]
    pub fn port(&self) -> u16 {
        self.port
    }

    #[napi(getter)]
    pub fn connection_count(&self) -> u32 {
        self.connection_count.load(Ordering::Relaxed)
    }
}