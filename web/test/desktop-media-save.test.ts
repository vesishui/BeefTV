import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { afterEach, describe, expect, test } from "bun:test";

import { ownedResourceIdFromMediaRef } from "@/services/api/resources";
import { configureApiRuntime } from "@/services/api/request";
import { canStreamOwnedArchive, downloadOwnedOrBrowserMedia, isWailsNativeShell, saveOwnedOrBrowserBlob, saveOwnedArchiveEntries } from "@/services/desktop-media-save";
import { sanitizeDownloadFileName } from "@/lib/canvas/canvas-media-download";

const originalWindow = globalThis.window;
const originalFetch = globalThis.fetch;

afterEach(() => {
    globalThis.window = originalWindow;
    globalThis.fetch = originalFetch;
    configureApiRuntime("/api", "");
});

describe("native media save", () => {
    test("extracts owned resource IDs and rejects arbitrary remote URLs", () => {
        configureApiRuntime("http://127.0.0.1:43123/api", "token");
        expect(ownedResourceIdFromMediaRef("resource:abc123")).toBe("abc123");
        expect(ownedResourceIdFromMediaRef(undefined, "http://127.0.0.1:43123/api/resources/abc123/file")).toBe("abc123");
        expect(ownedResourceIdFromMediaRef(undefined, "/api/resources/abc123/file")).toBe("abc123");
        expect(ownedResourceIdFromMediaRef(undefined, "https://cdn.example.com/api/resources/abc123/file")).toBe("");
        expect(ownedResourceIdFromMediaRef(undefined, "data:video/mp4;base64,AAAA")).toBe("");
        expect(ownedResourceIdFromMediaRef(undefined, "blob:http://127.0.0.1:3000/abc")).toBe("");
    });

    test("sanitizes download file names", () => {
        expect(sanitizeDownloadFileName("../evil:name?.mp4")).toBe("evil_name.mp4");
        expect(sanitizeDownloadFileName("镜头 01.MP4")).toBe("镜头 01.mp4");
    });

    test("native shell uses the Wails save binding and treats empty path as cancel", async () => {
        const calls: Array<[string, string]> = [];
        Object.assign(globalThis, {
            window: {
                location: { protocol: "wails:" },
                go: {
                    main: {
                        DesktopApp: {
                            SaveOwnedMedia: async (fileName: string, resourceID: string) => {
                                calls.push([fileName, resourceID]);
                                return false;
                            },
                        },
                    },
                },
            },
        });
        expect(isWailsNativeShell()).toBe(true);
        expect(await downloadOwnedOrBrowserMedia({ fileName: "clip.mp4", resourceId: "abc123", browserUrl: "https://example.com/secret.mp4" })).toBe("cancelled");
        expect(calls).toEqual([["clip.mp4", "abc123"]]);
    });

    test("native shell does not fetch remote URLs when a resource id is missing", async () => {
        let fetched = false;
        globalThis.fetch = (async () => {
            fetched = true;
            return new Response("nope");
        }) as typeof fetch;
        Object.assign(globalThis, {
            window: {
                location: { protocol: "wails:" },
                go: { main: { DesktopApp: { SaveOwnedMedia: async () => true } } },
            },
        });
        await expect(downloadOwnedOrBrowserMedia({ fileName: "clip.mp4", browserUrl: "https://example.com/video.mp4" })).rejects.toThrow("没有可导出的本机文件");
        expect(fetched).toBe(false);
    });

    test("browser path keeps saveAs and does not require a Wails binding", () => {
        Object.assign(globalThis, { window: { location: { protocol: "http:" } } });
        expect(isWailsNativeShell()).toBe(false);
        const nodeEditor = readFileSync(resolve(import.meta.dir, "../src/pages/canvas/use-canvas-node-editor.ts"), "utf8");
        const assets = readFileSync(resolve(import.meta.dir, "../src/pages/assets/index.tsx"), "utf8");
        const projectAssets = readFileSync(resolve(import.meta.dir, "../src/pages/projects/detail/assets.tsx"), "utf8");
        expect(nodeEditor).toContain("downloadOwnedOrBrowserMedia");
        expect(nodeEditor).not.toContain("saveAs(");
        expect(assets).toContain("downloadOwnedOrBrowserMedia");
        expect(projectAssets).toContain("downloadOwnedOrBrowserMedia");
        const runtime = readFileSync(resolve(import.meta.dir, "../src/services/desktop-runtime.ts"), "utf8");
        expect(runtime).toContain("SaveOwnedMedia");
        expect(runtime).toContain("SaveOwnedArtifact");
        const helper = readFileSync(resolve(import.meta.dir, "../src/services/desktop-media-save.ts"), "utf8");
        expect(helper).toContain("saveAs(browserUrl, fileName)");
        expect(helper).toContain("SaveOwnedArtifact");
        expect(helper).toContain("bytesToBase64");
        expect(helper).not.toContain("Array.from(bytes)");
        expect(helper).toContain("32 * 1024 * 1024");
        expect(readFileSync(resolve(import.meta.dir, "../src/lib/canvas/canvas-export.ts"), "utf8")).toContain("saveOwnedOrBrowserBlob");
        expect(readFileSync(resolve(import.meta.dir, "../src/pages/assets/asset-transfer.ts"), "utf8")).toContain("saveOwnedOrBrowserBlob");
        expect(readFileSync(resolve(import.meta.dir, "../src/components/canvas/canvas-timeline-dialog.tsx"), "utf8")).toContain("saveAs(blob");
        const saveGo = readFileSync(resolve(import.meta.dir, "../../backend/internal/bootstrap/owned_media_save.go"), "utf8");
        expect(saveGo).toContain("os.Rename(tmpPath, dest)");
        expect(saveGo).not.toContain(".beeftv-old");
        expect(saveGo).toContain("32 << 20");
    });

    test("streamed archive is only offered when every session binding exists", () => {
        Object.assign(globalThis, { window: { location: { protocol: "http:" } } });
        expect(canStreamOwnedArchive()).toBe(false);
        Object.assign(globalThis, {
            window: { location: { protocol: "wails:" }, go: { main: { DesktopApp: { SaveOwnedArtifact: async () => true } } } },
        });
        // 老版本应用只有一次性保存接口，必须回退而不是静默丢文件。
        expect(canStreamOwnedArchive()).toBe(false);
        Object.assign(globalThis, {
            window: {
                location: { protocol: "wails:" },
                go: { main: { DesktopApp: {
                    BeginOwnedArchive: async () => "session",
                    BeginOwnedArchiveEntry: async () => undefined,
                    AppendOwnedArchiveChunk: async () => undefined,
                    EndOwnedArchiveEntry: async () => undefined,
                    AppendOwnedArchiveResource: async () => undefined,
                    FinishOwnedArchive: async () => true,
                } } },
            },
        });
        expect(canStreamOwnedArchive()).toBe(true);
    });

    test("streamed archive registers local resources and chunks only in-memory entries", async () => {
        const calls: Array<[string, string]> = [];
        const chunks: Array<[string, string]> = [];
        const entries: string[] = [];
        Object.assign(globalThis, {
            window: {
                location: { protocol: "wails:" },
                go: { main: { DesktopApp: {
                    BeginOwnedArchive: async (fileName: string) => { calls.push(["begin", fileName]); return "session-1"; },
                    BeginOwnedArchiveEntry: async (_session: string, name: string) => { entries.push(name); },
                    AppendOwnedArchiveChunk: async (_session: string, data: string) => { chunks.push([entries[entries.length - 1], data]); },
                    EndOwnedArchiveEntry: async () => undefined,
                    AppendOwnedArchiveResource: async (_session: string, name: string, resourceId: string) => { calls.push([name, resourceId]); },
                    FinishOwnedArchive: async () => true,
                } } },
            },
        });

        const large = new Blob([new Uint8Array(9 * 1024 * 1024)], { type: "image/png" });
        const manifest = new Blob(['{"app":"infinite-canvas"}'], { type: "application/json" });
        expect(await saveOwnedArchiveEntries("画布.zip", [
            { name: "projects.json", data: manifest },
            { name: "projects/canvas/files/resource:abc.png", resourceId: "abc" },
            { name: "projects/canvas/files/image:local.png", data: large },
        ])).toBe("saved");

        expect(calls[0]).toEqual(["begin", "画布.zip"]);
        // 资源条目只传 ID，9MiB 的本地 blob 则按分片上限切分。
        expect(calls.slice(1)).toEqual([["projects/canvas/files/resource:abc.png", "abc"]]);
        expect(chunks.filter(([name]) => name === "projects/canvas/files/image:local.png")).toHaveLength(3);
        expect(chunks.find(([name]) => name === "projects.json")?.[1]).toBe(Buffer.from('{"app":"infinite-canvas"}').toString("base64"));
    });

    test("streamed archive reports cancellation and aborts on failure", async () => {
        let aborted = 0;
        Object.assign(globalThis, {
            window: {
                location: { protocol: "wails:" },
                go: { main: { DesktopApp: {
                    BeginOwnedArchive: async () => "",
                    BeginOwnedArchiveEntry: async () => undefined,
                    AppendOwnedArchiveChunk: async () => undefined,
                    EndOwnedArchiveEntry: async () => undefined,
                    AppendOwnedArchiveResource: async () => undefined,
                    FinishOwnedArchive: async () => true,
                    AbortOwnedArchive: async () => { aborted += 1; },
                } } },
            },
        });
        // 空会话 ID 表示用户在原生面板里取消。
        expect(await saveOwnedArchiveEntries("画布.zip", [])).toBe("cancelled");
        expect(aborted).toBe(0);

        Object.assign(globalThis, {
            window: {
                location: { protocol: "wails:" },
                go: { main: { DesktopApp: {
                    BeginOwnedArchive: async () => "session-2",
                    BeginOwnedArchiveEntry: async () => undefined,
                    AppendOwnedArchiveChunk: async () => { throw new Error("无法写出压缩包"); },
                    EndOwnedArchiveEntry: async () => undefined,
                    AppendOwnedArchiveResource: async () => undefined,
                    FinishOwnedArchive: async () => true,
                    AbortOwnedArchive: async () => { aborted += 1; },
                } } },
            },
        });
        await expect(saveOwnedArchiveEntries("画布.zip", [{ name: "projects.json", data: new Blob(["{}"]) }])).rejects.toThrow("无法写出压缩包");
        expect(aborted).toBe(1);
    });

    test("native ZIP artifacts use the bounded Wails save binding", async () => {
        const calls: Array<[string, string]> = [];
        Object.assign(globalThis, {
            window: {
                location: { protocol: "wails:" },
                go: {
                    main: {
                        DesktopApp: {
                            SaveOwnedArtifact: async (fileName: string, data: string) => {
                                calls.push([fileName, data]);
                                return true;
                            },
                        },
                    },
                },
            },
        });
        const blob = new Blob([new Uint8Array([1, 2, 3])], { type: "application/zip" });
        expect(await saveOwnedOrBrowserBlob("画布.zip", blob)).toBe("saved");
        expect(calls[0]?.[0]).toBe("画布.zip");
        expect(calls[0]?.[1]).toBe("AQID");
    });
});
