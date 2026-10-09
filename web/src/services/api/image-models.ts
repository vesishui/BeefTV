import { sanitizeChannelModelCatalogItem, type ChannelModelCatalogItem } from "@/lib/channel-model-catalog";
import { createChannelTransport } from "@/services/api/channel-transport";
import { readAxiosError, validateGeminiPayload } from "@/services/api/image-response";
import { geminiApiUrl, geminiHeaders } from "@/services/api/image-transport";
import { ApiError, http } from "@/services/api/request";
import { buildApiUrl, type AiConfig, type ModelChannel } from "@/stores/use-config-store";

const defaultGeminiConfig: Pick<AiConfig, "baseUrl" | "apiKey" | "apiFormat" | "model" | "systemPrompt"> = {
    baseUrl: "https://generativelanguage.googleapis.com",
    apiKey: "",
    apiFormat: "gemini",
    model: "",
    systemPrompt: "",
};

type GeminiModelPayload = { models?: Array<{ name?: string }> };
type OpenAIModelRecord = {
	id?: string;
	name?: string;
	display_name?: string;
	model_type?: string;
	capability?: string;
	type?: string;
	supported_endpoint_types?: string[];
	video_capabilities?: unknown;
	video_capabilities_version?: string;
};
type OpenAIModelBucket = Array<string | OpenAIModelRecord>;
type OpenAIModelPayload = { data?: OpenAIModelRecord[] | Record<string, OpenAIModelBucket>; error?: { message?: string } };

const capabilityBuckets = new Set(["video", "image", "audio", "chat", "other"]);

function modelTypeForBucket(bucket: string) {
	return bucket === "chat" ? "text" : capabilityBuckets.has(bucket) && bucket !== "other" ? bucket : undefined;
}

export function parseOpenAIModelCatalogPayload(payload: OpenAIModelPayload): ChannelModelCatalogItem[] {
	const data = payload.data;
	const records: OpenAIModelRecord[] = Array.isArray(data)
		? data
		: Object.entries(data || {}).flatMap(([bucket, values]) => {
				const modelType = modelTypeForBucket(bucket);
				return (Array.isArray(values) ? values : []).map((value) =>
					typeof value === "string"
						? { id: value, model_type: modelType }
						: { ...value, model_type: modelValueModelType(value, modelType) },
				);
			});
	return records
		.map((model) =>
			sanitizeChannelModelCatalogItem({
				id: model.id || model.name,
				displayName: model.display_name || model.name,
				modelType: modelValueModelType(model),
				supportedEndpointTypes: model.supported_endpoint_types,
				videoCapabilities: model.video_capabilities,
				videoCapabilitiesVersion: model.video_capabilities_version,
			}),
		)
		.filter((item): item is ChannelModelCatalogItem => Boolean(item));
}

function modelValueModelType(model: OpenAIModelRecord, fallback?: string) {
	const value = firstNonEmptyText(model.model_type, model.capability, model.type, fallback);
	return value === "chat" ? "text" : value;
}

function firstNonEmptyText(...values: Array<string | undefined>) {
	for (const value of values) {
		const text = value?.trim();
		if (text) return text;
	}
	return undefined;
}

async function fetchOpenAIModelCatalog(config: Pick<ModelChannel, "baseUrl" | "apiKey" | "apiFormat" | "headers" | "credentialRef">) {
    const payload = await createChannelTransport(config, "image").get<OpenAIModelPayload>(buildApiUrl(config.baseUrl, "/models"));
    return parseOpenAIModelCatalogPayload(payload);
}

export async function fetchImageModels(config: Pick<AiConfig, "baseUrl" | "apiKey" | "apiFormat">) {
    try {
        if (config.apiFormat === "gemini") {
            const requestConfig = { ...defaultGeminiConfig, ...config };
            const payload = await createChannelTransport(requestConfig, "image").get<GeminiModelPayload>(geminiApiUrl(requestConfig), { headers: geminiHeaders(requestConfig) });
            validateGeminiPayload(payload);
            return (payload.models || [])
                .map((model) => model.name?.replace(/^models\//, ""))
                .filter((id): id is string => Boolean(id))
                .sort((a, b) => a.localeCompare(b));
        }
        const catalog = await fetchOpenAIModelCatalog(config);
        return catalog.map((model) => model.id).sort((a, b) => a.localeCompare(b));
    } catch (error) {
        throw new Error(readAxiosError(error, "读取模型失败"));
    }
}

export type ChannelModelFetchResult = { models: string[]; catalog: ChannelModelCatalogItem[] };

export async function fetchChannelModels(channel: ModelChannel, viaBackend = false): Promise<ChannelModelFetchResult> {
    const managed = channel.id === "beefapi" && (channel.pinned || Boolean(channel.credentialRef));
    if (managed) {
        viaBackend = true;
    }
    if (!viaBackend) {
        if (channel.apiFormat !== "gemini") {
            const catalog = await fetchOpenAIModelCatalog(channel);
            return { models: catalog.map((item) => item.id).sort((a, b) => a.localeCompare(b)), catalog };
        }
        const models = await fetchImageModels(channel);
        return { models, catalog: models.map((id) => ({ id })) };
    }
    try {
        // 登录态由同源后端代取模型目录，避免每个 OpenAI 兼容服务分别维护浏览器 CORS 白名单。
        const result = await http.post<{ models?: Array<string | ChannelModelCatalogItem> }>("/ai/models", {
            baseUrl: channel.baseUrl,
            apiKey: managed ? "" : channel.apiKey,
            apiFormat: channel.apiFormat,
            headers: channel.headers,
            channelId: managed ? channel.id : undefined,
            credentialRef: managed ? "beefapi-enterprise" : undefined,
        }, { timeout: 30_000 });
        const catalog = new Map<string, ChannelModelCatalogItem>();
        for (const item of result.models || []) {
            const entry = typeof item === "string" ? sanitizeChannelModelCatalogItem({ id: item }) : sanitizeChannelModelCatalogItem(item);
            if (!entry) continue;
            const existing = catalog.get(entry.id);
            catalog.set(entry.id, existing || entry);
        }
        const models = Array.from(catalog.keys()).sort((a, b) => a.localeCompare(b));
        const sortedCatalog = Array.from(catalog.values()).sort((a, b) => a.id.localeCompare(b.id));
        return { models, catalog: sortedCatalog };
    } catch (error) {
        // The backend has already classified and sanitized catalogue errors.
        // Running generation-error classification again turns a precise 502
        // auth/unsupported-catalog message into a generic unavailable message.
        if (error instanceof ApiError) throw error;
        throw new Error(readAxiosError(error, "读取模型失败"));
    }
}
