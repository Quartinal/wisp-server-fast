use std::io::Cursor;
use std::pin::Pin;
use std::task::{Context, Poll};

use tokio::io::{AsyncRead, AsyncWrite, ReadBuf};
use tokio::net::TcpStream;
use tokio_tungstenite::{accept_async, WebSocketStream};
use tokio_tungstenite::tungstenite::Error as WsError;

use wisp_mux::ws::{TokioTungsteniteTransport, TransportExt, WebSocketSplitRead, WebSocketSplitWrite};

pub type Transport = TokioTungsteniteTransport<TcpStream>;
pub type SplitRx   = WebSocketSplitRead<Transport>;
pub type SplitTx   = WebSocketSplitWrite<Transport>;

pub async fn upgrade_and_split(
    stream: TcpStream,
    head: Vec<u8>,
) -> Result<(SplitRx, SplitTx), WsError> {
    debug_assert!(head.is_empty(), "unexpected non-empty head in WS upgrade");

    let ws: WebSocketStream<TcpStream> = accept_async(stream).await?;
    Ok(TokioTungsteniteTransport(ws).split_fast())
}