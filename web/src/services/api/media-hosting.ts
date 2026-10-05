/**
 * 素材图床配置接口。
 *
 * 图床把本地参考素材转成上游可下载的公网地址。图片、视频、音频共用这套配置，
 * 与小七那套只支持图片的实现不同。
 *
 * 密钥（customToken / accessKeyId / secretAccessKey 等）由后端加密保存，读接口
 * 只返回 __BEEFTV_REDACTED__ 占位符；保存时回传占位符表示沿用已存密钥。
 */
import { http } from "@/services/api/request";

export type MediaHostingProvider =
    | "none"
    | "builtin"
    | "free"
    | "custom"
    | "aliyun-oss"
    | "tencent-cos"
    | "cloudflare-r2"
    | "aws-s3";

export type MediaHostingFreeProvider = "auto" | "litterbox" | "uguu" | "imgbb";

export type MediaHostingKind = "image" | "video" | "audio";

export type MediaHostingConfig = {
    provider: MediaHostingProvider;
    freeProvider?: MediaHostingFreeProvider;
    freeExpiry?: string;
    imgbbApiKey?: string;
    endpoint?: string;
    customFileField?: string;
    customUrlPath?: string;
    customAuthHeader?: string;
    customAuthScheme?: string;
    customToken?: string;
    bucket?: string;
    region?: string;
    accountId?: string;
    accessKeyId?: string;
    secretAccessKey?: string;
    sessionToken?: string;
    publicBaseUrl?: string;
    pathPrefix?: string;
};

export type MediaHostingPayload = {
    config: MediaHostingConfig;
    enabled: boolean;
    cachedCount: number;
};

/** 后端用于替代密钥的占位符；回传表示保留已存密钥。 */
export const MEDIA_HOSTING_REDACTED = "__BEEFTV_REDACTED__";

export function getMediaHostingConfig() {
    return http.get<MediaHostingPayload>("/workspace/media-hosting");
}

export function saveMediaHostingConfig(config: MediaHostingConfig) {
    return http.put<{ config: MediaHostingConfig; enabled: boolean }>("/workspace/media-hosting", { config });
}

export type MediaHostingTestResult = {
    url: string;
    kind: MediaHostingKind;
    provider: MediaHostingProvider;
    bytes: number;
};

/** 用一小段合成素材探测图床是否可用，不会真正发起生成任务。 */
export function testMediaHosting(kind: MediaHostingKind = "image", signal?: AbortSignal) {
    return http.post<MediaHostingTestResult>("/workspace/media-hosting/test", undefined, {
        params: { kind },
        signal,
    });
}

/** 每种图床能承载的素材类型。免费公共图床不收视频。 */
export function providerSupportsKind(provider: MediaHostingProvider, kind: MediaHostingKind): boolean {
    switch (provider) {
        case "none":
            return false;
        case "free":
            return kind === "image" || kind === "audio";
        case "builtin":
        case "custom":
        case "aliyun-oss":
        case "tencent-cos":
        case "cloudflare-r2":
        case "aws-s3":
            return true;
        default:
            return false;
    }
}

export const MEDIA_HOSTING_PROVIDER_LABELS: Record<MediaHostingProvider, string> = {
    none: "关闭",
    builtin: "内置图床",
    free: "免费图床",
    custom: "自定义",
    "aliyun-oss": "阿里云 OSS",
    "tencent-cos": "腾讯云 COS",
    "cloudflare-r2": "Cloudflare R2",
    "aws-s3": "AWS S3",
};
