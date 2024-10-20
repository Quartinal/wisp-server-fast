import { build } from "esbuild";
import { mkdir, rm } from "fs/promises";

await rm("dist", { recursive: true, force: true });
await mkdir("dist");

await build({
    entryPoints: {
        "index": "./index.ts"
    },
    minify: true,
    format: "esm",
    bundle: true,
    logLevel: "info",
    outdir: "dist/",
    platform: "node",
});