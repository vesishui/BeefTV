import { captureUserScope, userScopeMatches, type CapturedUserScope } from "@/lib/user-scope-guard";

import type { CanvasOwnerEpoch, CanvasOwnerLifetime } from "./canvas-owner-epoch";

/**
 * 画布编辑器的节点状态是跨画布共享的：`/canvas/:id` 路由没有 key，切换画布不会
 * 重挂载页面，`setNodes` / `nodesRef` 一直是同一个实例。
 *
 * 所以任何异步生成写入在真正落笔前，都必须重新确认「编辑器还停在发起这次生成的
 * 画布上」。只判断消费器 AbortSignal 是不够的：切画布时它被替换成新画布的
 * controller（新 controller 并未 abort），旧判断依然成立，于是上一张画布的整份
 * 节点数组会被写进编辑器状态，再被自动保存落到新打开的画布上——表现为「第二集
 * 被插入了几十个出图素材」，而连线仍是第二集自己的（因为写入只覆盖 nodes）。
 */
export type CanvasGenerationOwnership = {
    owner: CanvasOwnerEpoch;
    capturedScope: CapturedUserScope;
    /** 编辑器是否仍然停在发起本次生成的画布上。 */
    isCurrent: () => boolean;
};

export function captureCanvasGenerationOwnership(input: { canvasId: string; lifetime: CanvasOwnerLifetime; getLiveCanvasId: () => string; signal?: AbortSignal }): CanvasGenerationOwnership {
    const owner = input.lifetime.capture(input.canvasId);
    const capturedScope = captureUserScope();
    return {
        owner,
        capturedScope,
        isCurrent() {
            if (input.signal?.aborted) return false;
            const liveCanvasId = input.getLiveCanvasId();
            // 画布身份必须完全一致：同 id 重挂载由 lifetime 拦下，账号切换由 scope 拦下。
            if (!liveCanvasId || liveCanvasId !== owner.canvasId) return false;
            if (!input.lifetime.matches(owner, liveCanvasId)) return false;
            return userScopeMatches(capturedScope);
        },
    };
}
