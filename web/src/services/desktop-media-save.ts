import { saveAs } from "file-saver";

import { sanitizeDownloadFileName } from "@/lib/canvas/canvas-media-download";

export type OwnedMediaSaveResult = "saved" | "cancelled";

export const MAX_OWNED_ARTIFACT_BYTES = 32 * 1024 * 1024;

export function isWailsNativeShell() {
    if (typeof window === "undefined") return false;
    return window.location?.protocol === "wails:" || typeof window.go?.main?.DesktopApp?.SaveOwnedMedia === "function" || typeof window.go?.main?.DesktopApp?.SaveOwnedArtifact === "function";
}

export async function downloadOwnedOrBrowserMedia(options: { fileName: string; resourceId?: string; browserUrl?: string }): Promise<OwnedMediaSaveResult> {
    const fileName = sanitizeDownloadFileName(options.fileName);
    if (isWailsNativeShell()) {
        const resourceId = options.resourceId?.trim();
        if (!resourceId) throw new Error("没有可导出的本机文件");
        const save = window.go?.main?.DesktopApp?.SaveOwnedMedia;
        if (!save) throw new Error("当前应用还不能把文件存到所选位置");
        const saved = await save(fileName, resourceId);
        return saved ? "saved" : "cancelled";
    }
    const browserUrl = options.browserUrl?.trim();
    if (!browserUrl) throw new Error("没有可导出的文件");
    saveAs(browserUrl, fileName);
    return "saved";
}

export async function saveOwnedOrBrowserBlob(fileName: string, blob: Blob): Promise<OwnedMediaSaveResult> {
    const name = sanitizeDownloadFileName(fileName, "未命名导出");
    if (isWailsNativeShell()) {
        if (blob.size > MAX_OWNED_ARTIFACT_BYTES) throw new Error("导出包太大，请减少所选内容后再导出");
        const save = window.go?.main?.DesktopApp?.SaveOwnedArtifact;
        if (!save) throw new Error("当前应用还不能把文件存到所选位置");
        const bytes = new Uint8Array(await blob.arrayBuffer());
        const saved = await save(name, bytesToBase64(bytes));
        return saved ? "saved" : "cancelled";
    }
    saveAs(blob, name);
    return "saved";
}

function bytesToBase64(bytes: Uint8Array) {
    const toBase64 = (bytes as Uint8Array & { toBase64?: () => string }).toBase64;
    if (typeof toBase64 === "function") return toBase64.call(bytes);
    const chunkSize = 0x8000;
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += chunkSize) {
        binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
    }
    return btoa(binary);
}

/**
 * 一个归档条目：要么由后端直接从本机资源库流式写入（resourceId），
 * 要么是只能由前端提供的小文件（清单、画板文档等）。
 */
export type OwnedArchiveEntry = { name: string; resourceId?: string; data?: Blob };

/** 单次 IPC 分片；4MiB 原始数据约 5.4MiB base64，足以限制 WebView 内存峰值。 */
const ARCHIVE_CHUNK_BYTES = 4 * 1024 * 1024;

/**
 * 流式导出是否可用。
 *
 * 只有桌面壳且后端提供了归档会话绑定才能走这条路；否则调用方必须回退到
 * 一次性打包，避免在老版本应用里静默丢文件。
 */
export function canStreamOwnedArchive() {
    if (!isWailsNativeShell()) return false;
    const binding = window.go?.main?.DesktopApp;
    return typeof binding?.BeginOwnedArchive === "function"
        && typeof binding.BeginOwnedArchiveEntry === "function"
        && typeof binding.AppendOwnedArchiveChunk === "function"
        && typeof binding.EndOwnedArchiveEntry === "function"
        && typeof binding.AppendOwnedArchiveResource === "function"
        && typeof binding.FinishOwnedArchive === "function";
}

/**
 * 把条目写入用户选择的 ZIP，全程不在前端堆出整个压缩包。
 *
 * 媒体条目由后端按资源 ID 从磁盘直接拷入；只有前端独有的小文件才分片传输。
 * 任何失败都会中止会话并删除临时文件，已存在的目标文件不会被截断。
 */
export async function saveOwnedArchiveEntries(fileName: string, entries: OwnedArchiveEntry[]): Promise<OwnedMediaSaveResult> {
    const binding = window.go?.main?.DesktopApp;
    if (!binding?.BeginOwnedArchive || !binding.BeginOwnedArchiveEntry || !binding.AppendOwnedArchiveChunk
        || !binding.EndOwnedArchiveEntry || !binding.AppendOwnedArchiveResource || !binding.FinishOwnedArchive) {
        throw new Error("当前应用还不能把文件存到所选位置");
    }
    const name = sanitizeDownloadFileName(fileName, "未命名导出");
    const sessionID = await binding.BeginOwnedArchive(name);
    // 空会话 ID 表示用户在原生保存面板里取消，不是错误。
    if (!sessionID) return "cancelled";
    try {
        for (const entry of entries) {
            if (entry.resourceId) {
                await binding.AppendOwnedArchiveResource(sessionID, entry.name, entry.resourceId);
                continue;
            }
            if (!entry.data) throw new Error(`导出条目缺少内容：${entry.name}`);
            await binding.BeginOwnedArchiveEntry(sessionID, entry.name);
            const bytes = new Uint8Array(await entry.data.arrayBuffer());
            for (let offset = 0; offset < bytes.length; offset += ARCHIVE_CHUNK_BYTES) {
                await binding.AppendOwnedArchiveChunk(sessionID, bytesToBase64(bytes.subarray(offset, offset + ARCHIVE_CHUNK_BYTES)));
            }
            await binding.EndOwnedArchiveEntry(sessionID);
        }
        return (await binding.FinishOwnedArchive(sessionID)) ? "saved" : "cancelled";
    } catch (error) {
        // 会话必须释放，否则临时文件会一直留在目标目录里。
        await binding.AbortOwnedArchive?.(sessionID).catch(() => undefined);
        throw error;
    }
}

export function reportOwnedMediaSave(message: { success: (text: string) => void; error: (text: string) => void }, result: Promise<OwnedMediaSaveResult>) {
    return result
        .then((status) => {
            if (status === "saved" && isWailsNativeShell()) message.success("已保存到所选位置");
        })
        .catch((error) => {
            message.error(error instanceof Error && error.message.trim() ? error.message : "保存没有完成，请再试一次");
        });
}
