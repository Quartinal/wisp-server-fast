import express from "express";
import { initWasm, handleWebSocketConnection } from "../../sdk/node/dist";
import compression from "compression";
import http from "http";
import path from "path";

const app = express();
const server = http.createServer(app);

initWasm().then(() => {
    console.log("WebAssembly module initialized");
}).catch((error) => {
    console.error("Failed to initialize WebAssembly module:", error);
});

app.use("/wisp/", (req) => {
    handleWebSocketConnection(req);
});
app.use(express.static(path.join(import.meta.dirname, "../public")));
app.use(compression());

const PORT = 3000;
server.listen(PORT, () => {
    console.log(`Server running on http://localhost:${PORT}`);
});