"use strict"

const os = require("os")

const osName = os.platform()
const arch = os.arch()
const isGnu = (osName === "linux") ? "gnu" : ""

const formattedOS = [osName, arch, isGnu].filter(Boolean).join("-");

const { name: packageName } = require("./package.json")

const { EpoxyServer } = require(`${packageName}.${formattedOS}.node`);

class WispServer {
  #server

  constructor(options = {}) {
    this.#server = new EpoxyServer({
      bufferSize:     options.bufferSize     ?? null,
      wispV2:         options.wispV2         ?? null,
      maxConnections: options.maxConnections ?? null,
      allowedHosts:   options.allowedHosts   ?? null,
      blockedPorts:   options.blockedPorts   ?? null,
    })
  }

  get connectionCount() {
    return this.#server.connectionCount
  }

  attach(httpServer) {
    httpServer.on("upgrade", (req, socket, head) => {
      this.routeRequest(req, socket, head)
    })
    return this
  }

  routeRequest(req, socket, head) {
    socket.pause()

    const fd = socket._handle?.fd
    if (fd == null || fd === -1) {
      socket.destroy(new Error("[epoxy-server] could not get socket fd"))
      return
    }

    this.#server
      .routeConnection(BigInt(fd), head ?? Buffer.alloc(0))
      .catch(err => {
        if (!socket.destroyed) socket.destroy(err)
      })
  }
}

module.exports = { WispServer }