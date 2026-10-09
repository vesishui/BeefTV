import { afterEach, beforeEach, describe, expect, it } from "bun:test";

import { apiClient } from "../src/services/api/request";
import { hydrateProtocolCapabilities } from "../src/services/api/protocol-capabilities";
import { protocolCapabilityFlags, protocolRequiresStructuredMedia, resetProtocolCapabilities } from "../src/services/api/protocol-capabilities";

const originalApiRequest = apiClient.request;

beforeEach(() => {
    resetProtocolCapabilities();
    apiClient.request = originalApiRequest;
});

// 必须在每个用例后还原：否则最后一个用例装的 mock 会留给同一次运行的其它测试文件，
// 让依赖真实 apiClient 的用例（例如原生视频导出）莫名其妙地失败。
afterEach(() => {
    resetProtocolCapabilities();
    apiClient.request = originalApiRequest;
});

describe("protocol capability flags", () => {
    it("treats unknown protocols as plain model protocols", () => {
        expect(protocolRequiresStructuredMedia("sparkcine-h3-video")).toBe(false);
        expect(protocolRequiresStructuredMedia(undefined)).toBe(false);
        expect(protocolCapabilityFlags("openvideo-video")).toBeUndefined();
    });

    it("marks declarative protocols that require structured reference media", async () => {
        apiClient.request = (async () => ({
            status: 200,
            data: {
                code: 0,
                msg: "ok",
                data: {
                    providers: [
                        { id: "sparkcine-h3-video", execution: "declarative", requiresPublicMediaUrls: true },
                        { id: "openvideo-video", execution: "declarative", requiresPublicMediaUrls: true },
                        { id: "openai-videos", execution: "declarative" },
                        { id: "chat-completion" },
                    ],
                },
            },
        })) as typeof apiClient.request;

        await hydrateProtocolCapabilities();

        expect(protocolRequiresStructuredMedia("sparkcine-h3-video")).toBe(true);
        expect(protocolRequiresStructuredMedia("openvideo-video")).toBe(true);
        expect(protocolRequiresStructuredMedia("chat-completion")).toBe(false);
    });
});
