import type { IncomingMessage } from "http";

declare function initWasm(): Promise<void>;
declare function handleWebSocketConnection(jsWsConn: IncomingMessage): void;

export { handleWebSocketConnection, initWasm };