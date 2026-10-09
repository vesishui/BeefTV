import { http } from "@/services/api/request";

/**
 * 声明式协议的运行时能力标记。目录里的模型协议分两类：
 * - 普通模型协议：前端组装扁平 payload，厂商自己决定怎么用参考素材；
 * - 声明式协议：host 按 manifest 的字段映射组装请求，需要结构化的参考媒体。
 *
 * 后者由插件目录声明 requiresPublicMediaUrls/execution，前端据此决定生成时
 * 是否保留连接进来的参考图、音频、视频。这里只做标记缓存，协议目录加载失败
 * 时按“普通协议”处理，不阻塞画布使用。
 */

export type ProtocolCapabilityFlags = {
    execution?: string;
    requiresPublicMediaUrls?: boolean;
};

const flags = new Map<string, ProtocolCapabilityFlags>();

function isDeclarative(entry: ProtocolCapabilityFlags | undefined) {
    return entry?.execution === "declarative" || entry?.requiresPublicMediaUrls === true;
}

/** 该协议是否需要结构化参考素材（声明式多模态协议）。 */
export function protocolRequiresStructuredMedia(protocolId: string | undefined): boolean {
    const id = protocolId?.trim();
    if (!id) return false;
    return isDeclarative(flags.get(id));
}

export function protocolCapabilityFlags(protocolId: string | undefined): ProtocolCapabilityFlags | undefined {
    const id = protocolId?.trim();
    return id ? flags.get(id) : undefined;
}

type CatalogProvider = {
    id: string;
    execution?: string;
    requiresPublicMediaUrls?: boolean;
};

let hydrated: Promise<void> | undefined;

async function load() {
    try {
        const result = await http.get<{ providers: CatalogProvider[] }>("/plugins/catalog", { params: { scope: "user.custom-channel" } });
        for (const provider of result.providers || []) {
            const id = provider.id?.trim();
            if (!id) continue;
            if (provider.execution || provider.requiresPublicMediaUrls) {
                flags.set(id, { execution: provider.execution, requiresPublicMediaUrls: provider.requiresPublicMediaUrls });
            }
        }
    } catch {
        // 目录不可用时保持空表：生成链继续按普通协议处理，不把插件中心故障升级成画布故障。
    }
}

/** 幂等加载协议标记；由应用启动链调用，失败只记录一次空结果。 */
export function hydrateProtocolCapabilities(): Promise<void> {
    if (!hydrated) hydrated = load();
    return hydrated;
}

/** 仅供测试重置内存标记。 */
export function resetProtocolCapabilities() {
    flags.clear();
    hydrated = undefined;
}
