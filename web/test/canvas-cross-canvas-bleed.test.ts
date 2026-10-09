import { beforeEach, describe, expect, test } from "bun:test";

import { captureCanvasGenerationOwnership } from "@/pages/canvas/canvas-generation-ownership";
import { createCanvasOwnerLifetime } from "@/pages/canvas/canvas-owner-epoch";
import { bindBackendCanvasGenerationResult } from "@/services/canvas-generation-consumer";
import { captureUserScope, userScopeMatches } from "@/lib/user-scope-guard";
import { useCanvasStore, type CanvasProject } from "@/stores/canvas/use-canvas-store";
import { CanvasNodeType, type CanvasNodeData } from "@/types/canvas";
import type { GenerationTask } from "@/services/api/task-center";
import type { CanvasTaskBindReceipt } from "@/services/api/operations";

/**
 * 真实事故：在「生图」画布上生成图片，同时打开第二集画布。
 * 出图任务完成后，出图那张画布的**整份节点数组**被写进了第二集的编辑器状态，
 * 再被自动保存落到第二集——第二集被插入了几十个出图素材，而它自己的连线还在
 * （指向已不存在的节点），说明写入只覆盖了 nodes。
 */

const GENERATING_CANVAS = "F_uSDkMRoBUTIAb7sbN_x";
const SECOND_EPISODE_CANVAS = "8G3X5x2R92YQ3-4b3T8N-";

function node(id: string, title: string, metadata: CanvasNodeData["metadata"] = {}): CanvasNodeData {
    return { id, type: CanvasNodeType.Image, title, position: { x: 0, y: 0 }, width: 320, height: 220, metadata };
}

function project(id: string, nodes: CanvasNodeData[], revision = 3): CanvasProject {
    return {
        id,
        revision,
        title: id,
        createdAt: "2026-01-01T00:00:00.000Z",
        updatedAt: "2026-01-01T00:00:00.000Z",
        nodes,
        connections: [],
        chatSessions: [],
        activeChatId: null,
        backgroundMode: "grid",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
        directorScenes: [],
    };
}

function succeededTask(): GenerationTask {
    return {
        id: "task-1",
        type: "canvas_image",
        status: "succeeded",
        prompt: "猫",
        createdAt: "2026-01-01T00:00:00.000Z",
        updatedAt: "2026-01-01T00:00:00.000Z",
    } as GenerationTask;
}

/** 模拟 `useCanvasOwnerLifetime`：画布 id 变化时使旧纪元失效（页面不重挂载）。 */
function editorSession(initialCanvasId: string) {
    const lifetime = createCanvasOwnerLifetime();
    let liveCanvasId = initialCanvasId;
    return {
        lifetime,
        live: () => liveCanvasId,
        openCanvas(next: string) {
            liveCanvasId = next;
            lifetime.invalidate();
        },
    };
}

describe("canvas generation ownership across canvas switches", () => {
    test("stays current while the editor is on the generating canvas", () => {
        const session = editorSession(GENERATING_CANVAS);
        const ownership = captureCanvasGenerationOwnership({
            canvasId: GENERATING_CANVAS,
            lifetime: session.lifetime,
            getLiveCanvasId: session.live,
        });
        expect(ownership.isCurrent()).toBe(true);
    });

    test("stops being current once the editor opens another canvas", () => {
        const session = editorSession(GENERATING_CANVAS);
        const ownership = captureCanvasGenerationOwnership({
            canvasId: GENERATING_CANVAS,
            lifetime: session.lifetime,
            getLiveCanvasId: session.live,
        });
        session.openCanvas(SECOND_EPISODE_CANVAS);
        expect(ownership.isCurrent()).toBe(false);
    });

    test("an un-aborted consumer signal alone would have passed the stale check", () => {
        const session = editorSession(GENERATING_CANVAS);
        const controller = new AbortController();
        const capturedScope = captureUserScope();
        const ownership = captureCanvasGenerationOwnership({
            canvasId: GENERATING_CANVAS,
            lifetime: session.lifetime,
            getLiveCanvasId: session.live,
            signal: controller.signal,
        });
        // 切画布：旧 controller 未被 abort（它只是被替换成新画布的 controller）。
        session.openCanvas(SECOND_EPISODE_CANVAS);
        // 事故当时的判断条件——只有 abort + 账号作用域，因此为真。
        expect(!controller.signal.aborted && userScopeMatches(capturedScope)).toBe(true);
        // 修复后的判断条件。
        expect(ownership.isCurrent()).toBe(false);
    });

    test("rejects a remount of the same canvas and an account switch", () => {
        const session = editorSession(GENERATING_CANVAS);
        const ownership = captureCanvasGenerationOwnership({
            canvasId: GENERATING_CANVAS,
            lifetime: session.lifetime,
            getLiveCanvasId: session.live,
        });
        // 同 id 重新打开（返回上一页）→ 旧纪元失效。
        session.openCanvas(GENERATING_CANVAS);
        expect(ownership.isCurrent()).toBe(false);

        const aborted = captureCanvasGenerationOwnership({
            canvasId: GENERATING_CANVAS,
            lifetime: session.lifetime,
            getLiveCanvasId: session.live,
            signal: AbortSignal.abort(),
        });
        expect(aborted.isCurrent()).toBe(false);
    });
});

describe("bindBackendCanvasGenerationResult never writes the other canvas", () => {
    beforeEach(() => {
        useCanvasStore.setState({ projects: [] });
    });

    test("a result arriving after a canvas switch leaves the open canvas untouched", async () => {
        const session = editorSession(GENERATING_CANVAS);
        const generatingNodes = Array.from({ length: 49 }, (_, index) => node(`upload-${index}`, `出图素材 ${index}`));
        const generatingCanvas = project(GENERATING_CANVAS, generatingNodes, 1824);
        const secondEpisodeNodes = [node("video-1791513343060-ez0wb", "视频节点 1"), node("upload-1791513465889-gzcwj", "美女乙.png")];
        const secondEpisode = project(SECOND_EPISODE_CANVAS, secondEpisodeNodes, 15);
        useCanvasStore.setState({ projects: [generatingCanvas, secondEpisode] });

        // 编辑器当前显示的节点就是第二集的节点。
        const editorNodesRef = { current: secondEpisodeNodes };
        const editorWrites: CanvasNodeData[][] = [];

        const ownership = captureCanvasGenerationOwnership({
            canvasId: GENERATING_CANVAS,
            lifetime: session.lifetime,
            getLiveCanvasId: session.live,
        });
        // 用户在任务完成前切到了第二集。
        session.openCanvas(SECOND_EPISODE_CANVAS);

        const receipt: CanvasTaskBindReceipt = {
            applied: true,
            canvasId: GENERATING_CANVAS,
            nodeId: "upload-0",
            taskId: "task-1",
            bindingStatus: "bound",
            revision: 1825,
            canvas: project(
                GENERATING_CANVAS,
                generatingNodes.map((item, index) => (index === 0 ? { ...item, metadata: { ...item.metadata, status: "success" as const } } : item)),
                1825,
            ),
        };

        await bindBackendCanvasGenerationResult({
            canvasId: GENERATING_CANVAS,
            nodeId: "upload-0",
            task: succeededTask(),
            isCurrent: ownership.isCurrent,
            nodesRef: editorNodesRef,
            setNodes: (value) => {
                editorWrites.push(typeof value === "function" ? value(editorNodesRef.current) : value);
            },
            runtime: {
                hydrateOutputs: async () => undefined,
                persistDocument: async () => undefined,
                bindOutput: async (input) => ({ op: "canvas.task.bind", opId: input.operationId, replayed: false, revision: 1825, result: receipt }),
                adoptConfirmedProjection: async (incoming) => incoming,
            },
        });

        // 后端绑定照常完成，但第二集的编辑器状态一个字节都没被改写。
        expect(editorWrites).toEqual([]);
        expect(editorNodesRef.current).toBe(secondEpisodeNodes);
        expect(editorNodesRef.current.map((item) => item.id)).toEqual(secondEpisodeNodes.map((item) => item.id));
        // 第二集自己的连线仍然指向它自己的节点。
        expect(secondEpisode.connections).toEqual([]);
    });

    test("a result arriving while the generating canvas is still open is applied", async () => {
        const session = editorSession(GENERATING_CANVAS);
        const bound = node("node-1", "节点", { taskId: "task-1", status: "loading" });
        const generatingCanvas = project(GENERATING_CANVAS, [bound], 3);
        useCanvasStore.setState({ projects: [generatingCanvas] });
        const nodesRef = { current: [bound] };
        const writes: CanvasNodeData[][] = [];
        const ownership = captureCanvasGenerationOwnership({
            canvasId: GENERATING_CANVAS,
            lifetime: session.lifetime,
            getLiveCanvasId: session.live,
        });

        const receipt: CanvasTaskBindReceipt = {
            applied: true,
            canvasId: GENERATING_CANVAS,
            nodeId: "node-1",
            taskId: "task-1",
            bindingStatus: "bound",
            content: "/api/resources/res-1/file",
            storageKey: "resource:res-1",
            assetId: "generation_abc",
            revision: 4,
            canvas: project(GENERATING_CANVAS, [{ ...bound, metadata: { ...bound.metadata, status: "success", content: "/api/resources/res-1/file", storageKey: "resource:res-1", assetId: "generation_abc" } }], 4),
        };

        await bindBackendCanvasGenerationResult({
            canvasId: GENERATING_CANVAS,
            nodeId: "node-1",
            task: succeededTask(),
            isCurrent: ownership.isCurrent,
            nodesRef,
            setNodes: (value) => {
                const next = typeof value === "function" ? value(nodesRef.current) : value;
                nodesRef.current = next;
                writes.push(next);
            },
            runtime: {
                hydrateOutputs: async () => undefined,
                persistDocument: async () => undefined,
                bindOutput: async (input) => ({ op: "canvas.task.bind", opId: input.operationId, replayed: false, revision: 4, result: receipt }),
                adoptConfirmedProjection: async (incoming) => incoming,
            },
        });

        expect(writes.length).toBe(1);
        expect(nodesRef.current[0]?.metadata?.storageKey).toBe("resource:res-1");
    });
});
