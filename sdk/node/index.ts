/// <reference types="golang-wasm-exec" />
import { readFileSync } from "fs";
import "./wasm_exec.js";
import { join } from "path";
import type { IncomingMessage } from "http";

declare global {
    function handleWebSocket(jsWsConn: IncomingMessage): void;
}

const wasmPath = import.meta.dirname.endsWith("dist") 
    ? join(import.meta.dirname, "../wrapper.wasm") 
    : join(import.meta.dirname, "wrapper.wasm");
const wasmBuffer = Buffer.from(readFileSync(wasmPath));

let wasmInstance: WebAssembly.Instance | null = null;

export async function initWasm(): Promise<void> {
    //@ts-expect-error
    const go: Go = new global.Go();
    const result = await WebAssembly.instantiate(wasmBuffer, go.importObject);
    wasmInstance = result.instance;
    await go.run(wasmInstance);
}

export function handleWebSocketConnection(jsWsConn: IncomingMessage): void {
    if (!wasmInstance) {
        throw new Error("WebAssembly instance not initialized. Call initWasm() first.");
    }
    handleWebSocket(jsWsConn);
}