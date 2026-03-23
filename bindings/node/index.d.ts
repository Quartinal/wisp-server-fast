export interface WispServerOptions {
  bufferSize?:     number
  wispV2?:         boolean
  maxConnections?: number
  allowedHosts?:   string[]
  blockedPorts?:   number[]
}

export declare class WispServer {
  constructor(options?: WispServerOptions)

  readonly connectionCount: number

  attach(httpServer: import("http").Server): this

  routeRequest(
    req:    import("http").IncomingMessage,
    socket: import("net").Socket,
    head:   Buffer,
  ): void
}