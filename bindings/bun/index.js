"use strict"

const os = require("os")
const net = require("net")

const osName = os.platform()
const arch = os.arch()
const isGnu = (osName === "linux") ? "gnu" : ""

const formattedOS = [osName, arch, isGnu].filter(Boolean).join("-")

const { name: packageName } = require("./package.json")

const { EpoxyServer } = require(`${packageName}.${formattedOS}.node`)

const isBun = typeof Bun !== "undefined"

class WispServer {
  #server
  #port

  constructor(options = {}) {
    this.#server = new EpoxyServer({
      bufferSize:     options.bufferSize     ?? null,
      wispV2:         options.wispV2         ?? null,
      maxConnections: options.maxConnections ?? null,
      allowedHosts:   options.allowedHosts   ?? null,
      blockedPorts:   options.blockedPorts   ?? null,
    })
    this.#port = this.#server.port
  }

  get connectionCount() {
    return this.#server.connectionCount
  }

  get port() {
    return this.#port
  }

  attach(httpServer) {
    httpServer.on("upgrade", (req, socket, head) => {
      this.routeRequest(req, socket, head)
    })
    return this
  }

  routeRequest(req, socket, head) {
    socket.pause()

    const tcp = net.connect({ host: "127.0.0.1", port: this.#port }, () => {
      if (head?.length) tcp.write(head)
      socket.resume()
      socket.pipe(tcp)
      tcp.pipe(socket)
    })

    tcp.on("error", (err) => {
      console.error("[epoxy-server] tcp pipe error:", err)
      if (!socket.destroyed) socket.destroy()
    })

    socket.on("error", () => {
      if (!tcp.destroyed) tcp.destroy()
    })

    socket.on("close", () => {
      if (!tcp.destroyed) tcp.destroy()
    })

    tcp.on("close", () => {
      if (!socket.destroyed) socket.destroy()
    })
  }

  websocketHandler() {
    if (!isBun) {
      throw new Error(
        "[epoxy-server] websocketHandler() is only available in Bun. " +
        "Use attach() or routeRequest() for Node.js / express / fastify."
      )
    }

    const rustPort = this.#port

    return {
      async open(ws) {
        ws.data ??= {}

        try {
          const tcp = await Bun.connect({
            hostname: "127.0.0.1",
            port: rustPort,
            socket: {
              open(socket) {
                ws.data._tcp = socket
                if (ws.data._pending?.length) {
                  for (const chunk of ws.data._pending) socket.write(chunk)
                  ws.data._pending = null
                }
              },
              data(socket, chunk) {
                ws.sendBinary(chunk, true)
              },
              close() {
                if (!ws.data._closed) ws.close()
              },
              error(socket, err) {
                console.error("[epoxy-server-bun] tcp error:", err)
                if (!ws.data._closed) ws.close()
              },
            },
          })
          ws.data._tcp = tcp
        } catch (err) {
          console.error("[epoxy-server-bun] connect error:", err)
          ws.close()
        }
      },

      message(ws, msg) {
        const chunk = msg instanceof Buffer ? msg : Buffer.from(msg)
        if (ws.data?._tcp) {
          ws.data._tcp.write(chunk)
        } else {
          ws.data ??= {}
          ws.data._pending ??= []
          ws.data._pending.push(chunk)
        }
      },

      close(ws) {
        ws.data._closed = true
        ws.data?._tcp?.end()
      },

      drain() {},
    }
  }
}

module.exports = { WispServer }