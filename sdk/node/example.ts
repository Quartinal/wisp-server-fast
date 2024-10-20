import { initWasm, handleWebSocketConnection } from "./index.js";
import express from "express";
import http from "http";

const app = express();
const server = http.createServer(app);

initWasm().then(() => {
    console.log("WebAssembly module initialized");
}).catch((error) => {
    console.error("Failed to initialize WebAssembly module:", error);
});

app.use("/ws", (req) => {
    handleWebSocketConnection(req);
});

const PORT = 3000;
server.listen(PORT, () => {
    console.log(`Server running on http://localhost:${PORT}`);
});