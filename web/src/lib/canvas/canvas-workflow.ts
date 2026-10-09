import type { CanvasNodeMetadata } from "@/types/canvas";
import { protocolRequiresStructuredMedia } from "@/services/api/protocol-capabilities";

export type CanvasWorkflowProvider = "model" | "runninghub";

/**
 * 统一解析画布节点的工作流渠道；旧节点可能没有 workflowProvider，
 * 但已经保存了具体工作流 ID，此时不能退回普通模型的首尾帧逻辑。
 */
export function resolveCanvasWorkflowProvider(metadata?: CanvasNodeMetadata): CanvasWorkflowProvider {
    if (metadata?.workflowProvider) return metadata.workflowProvider;
    if (metadata?.runningHubWorkflowId?.trim()) return "runninghub";
    return "model";
}

export function isCanvasWorkflowProvider(metadata?: CanvasNodeMetadata) {
    const provider = resolveCanvasWorkflowProvider(metadata);
    return provider === "runninghub";
}

/**
 * 声明式多模态协议（例如 sparkcine-h3-video、openvideo-video）由 host 按
 * manifest 字段映射组装请求，参考图、音频、视频会被写进结构化字段。它们不能
 * 被当成普通模型只发送提示词，否则画布上连接好的参考素材会被静默丢弃。
 */
export function protocolUsesStructuredMedia(protocolId: string | undefined) {
    return protocolRequiresStructuredMedia(protocolId);
}
