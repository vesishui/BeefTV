import { isCanvasNodeGenerating } from "@/lib/canvas/canvas-node-task-state";
import { useCallback, useRef, type Dispatch, type SetStateAction } from "react";
import { App } from "antd";

import { buildNodeGenerationContext, hydrateNodeGenerationContext } from "@/components/canvas/canvas-node-generation";
import type { CanvasNodeGenerationMode } from "@/components/canvas/canvas-node-prompt-panel";
import { isGenerationCanceled } from "@/lib/canvas/canvas-project-generation";
import { buildConfirmedGenerationConfig } from "./canvas-assistant-proposal-execution";
import type { ConfirmedGenerationInputs } from "./canvas-assistant-proposal-snapshot";
import { canvasGenerationPromptMetadata, canvasGenerationRequestFingerprint, runCanvasGenerationSubmissionOnce } from "@/lib/canvas/canvas-generation-submission";
import { isGenerationTaskCapacityError } from "@/lib/canvas/canvas-generation-batch";
import { buildPortraitTexturePrompt } from "@/lib/canvas/canvas-portrait-texture";
import { buildCameraPrompt } from "@/lib/canvas/camera-prompt-library";
import { protocolUsesStructuredMedia } from "@/lib/canvas/canvas-workflow";
import { buildTextRewritePrompt } from "@/lib/prompts";
import { resolveCanvasStyleExecution } from "@/lib/canvas/canvas-style-execution";
import { generationErrorMessage } from "@/lib/generation-error";
import { navigateToSettings } from "@/lib/settings-navigation";
import { modelCompatibilityError, modelGroupReferenceLimits, modelPromptLengthError, modelRequestOptions, type ModelRequirements } from "@/lib/model-selection";
import type { Skill } from "@/services/api/skills";
import { skillRuntime } from "@/services/skill-runtime";
import type { GenerationTask } from "@/services/api/task-center";
import { useConfigStore, useEffectiveConfig, resolveModelChannel, resolveModelRequestConfig } from "@/stores/use-config-store";
import { seedanceReferenceRatioWarning } from "@/lib/seedance-channel-warning";
import type { Asset } from "@/stores/use-asset-store";
import { CanvasNodeType, type CanvasConnection, type CanvasNodeData } from "@/types/canvas";

import { executeImageGeneration } from "./canvas-image-generation-executor";
import { executeAudioGeneration, executeVideoGeneration } from "./canvas-media-generation-executors";
import { executeTextGeneration } from "./canvas-text-generation-executor";
import { canvasGenerationFailureMetadata, canvasGenerationRetryBlocked } from "./canvas-generation-failure";
import { useCanvasOwnerLifetime } from "./canvas-owner-epoch";
import { captureCanvasGenerationOwnership } from "./canvas-generation-ownership";
import { createReferenceLinkResolver } from "./canvas-reference-links";

type UseCanvasGenerationExecutorOptions = {
    projectId: string;
    domainProjectId?: string;
    addedSkills: Skill[];
    assets: Asset[];
    nodesRef: { current: CanvasNodeData[] };
    connectionsRef: { current: CanvasConnection[] };
    setNodes: Dispatch<SetStateAction<CanvasNodeData[]>>;
    setConnections: Dispatch<SetStateAction<CanvasConnection[]>>;
    setSelectedNodeIds: Dispatch<SetStateAction<Set<string>>>;
    setSelectedConnectionId: Dispatch<SetStateAction<string | null>>;
    setDialogNodeId: Dispatch<SetStateAction<string | null>>;
    setRunningNodeId: Dispatch<SetStateAction<string | null>>;
    startGenerationRequest: (targetNodeId: string, originNodeId: string, runningId?: string, controller?: AbortController) => AbortController;
    finishGenerationRequest: (targetNodeId: string, controller: AbortController) => void;
    bindGenerationTask: (targetNodeId: string, task: GenerationTask) => void;
    applyGenerationTaskResult: (targetNodeId: string, task: GenerationTask) => Promise<void>;
};

const NODE_STATUS_IDLE = "idle" as const;
const NODE_STATUS_LOADING = "loading" as const;
const NODE_STATUS_SUCCESS = "success" as const;
const NODE_STATUS_ERROR = "error" as const;

// 临时诊断：桌面版无法打开 DevTools，把生成链路的关键状态同时写到本地诊断接收端口。
function reportGenerationDiagnostic(stage: string, detail?: Record<string, unknown>) {
    try {
        void fetch("http://127.0.0.1:39217", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ stage, detail, at: new Date().toISOString() }),
        }).catch(() => {});
    } catch {
        // 诊断不影响生成链路。
    }
}

export type CanvasNodeGenerationOptions = {
    confirmedModelKey?: string;
    confirmedInputs?: ConfirmedGenerationInputs;
    /** 一次确认的稳定身份。重复提交必须复用，服务端据此回读原任务。 */
    clientOperationId?: string;
    controller?: AbortController;
    waitForTaskCapacity?: boolean;
    context?: { conversationId?: string; messageId?: string };
    retryContext?: { retryOf: string; attemptGroupId: string; clientOperationId: string };
    onTaskUpdate?: (task: GenerationTask) => void;
    skipDuplicateConfirmation?: boolean;
};

export function useCanvasGenerationExecutor({
    projectId,
    domainProjectId,
    addedSkills,
    assets,
    nodesRef,
    connectionsRef,
    setNodes,
    setConnections,
    setSelectedNodeIds,
    setSelectedConnectionId,
    setDialogNodeId,
    setRunningNodeId,
    startGenerationRequest,
    finishGenerationRequest,
    bindGenerationTask,
    applyGenerationTaskResult,
}: UseCanvasGenerationExecutorOptions) {
    const { message, modal } = App.useApp();
    // 画布切换不会重挂载编辑器：跨画布异步续跑必须自己确认编辑器还停在原画布上，
    // 否则会把另一张画布的节点/连线写进当前编辑器状态，并被自动保存落到当前画布。
    const currentProjectIdRef = useRef(projectId);
    currentProjectIdRef.current = projectId;
    const { lifetime: canvasOwnerLifetime } = useCanvasOwnerLifetime(projectId);
    const effectiveConfig = useEffectiveConfig();
    const isAiConfigReady = useConfigStore((state) => state.isAiConfigReady);
    const submissionLocksRef = useRef(new Map<string, Promise<unknown>>());
    const confirmDuplicateSubmission = useCallback(
        () =>
            new Promise<boolean>((resolve) => {
                modal.confirm({
                    title: "再次生成相同内容？",
                    content: "当前节点已使用相同提示词、模型、参数和参考素材提交过任务。再次生成会新建任务，并可能再次消耗积分。",
                    okText: "仍然生成",
                    cancelText: "取消",
                    centered: true,
                    onOk: () => resolve(true),
                    onCancel: () => resolve(false),
                });
            }),
        [modal],
    );

    return useCallback(
        (nodeId: string, mode: CanvasNodeGenerationMode, prompt: string, options?: CanvasNodeGenerationOptions) =>
            runCanvasGenerationSubmissionOnce(
                submissionLocksRef.current,
                nodeId,
                async () => {
                    // 本次生成归属的画布；后续所有编辑器写入都必须先确认它仍是当前画布。
                    const originOwnership = captureCanvasGenerationOwnership({
                        canvasId: projectId,
                        lifetime: canvasOwnerLifetime,
                        getLiveCanvasId: () => currentProjectIdRef.current,
                    });
                    const setNodesForOrigin: Dispatch<SetStateAction<CanvasNodeData[]>> = (value) => {
                        if (originOwnership.isCurrent()) setNodes(value);
                    };
                    const setConnectionsForOrigin: Dispatch<SetStateAction<CanvasConnection[]>> = (value) => {
                        if (originOwnership.isCurrent()) setConnections(value);
                    };
                    const inputNodes = options?.confirmedInputs?.nodes ?? nodesRef.current;
                    const inputConnections = options?.confirmedInputs?.connections ?? connectionsRef.current;
                    const inputConfig = options?.confirmedInputs?.config ?? effectiveConfig;
                    const inputAssets = options?.confirmedInputs?.assets ?? assets;
                    const inputSkills = options?.confirmedInputs?.skills ?? addedSkills;
                    const sourceNode = inputNodes.find((node) => node.id === nodeId);
                    reportGenerationDiagnostic("enter", {
                        nodeId,
                        mode,
                        promptChars: prompt.length,
                        sourceNodeFound: Boolean(sourceNode),
                        sourceNodeType: sourceNode?.type,
                        sourceNodeModel: sourceNode?.metadata?.model,
                        sourceNodeStatus: sourceNode?.metadata?.status,
                    });
                    if (isCanvasNodeGenerating(nodesRef.current.find((node) => node.id === nodeId))) {
                        reportGenerationDiagnostic("early-return-generating");
                        message.info("该节点的生成任务仍在进行中，请等待完成后再生成");
                        return;
                    }
                    if (sourceNode?.type === CanvasNodeType.Video && sourceNode.metadata?.videoEditOperation === "concat") {
                        reportGenerationDiagnostic("early-return-concat");
                        message.info("合并成片节点不直接重新生成，请重新选择源视频合并");
                        return;
                    }
                    let generationConfig: ReturnType<typeof buildConfirmedGenerationConfig>;
                    try {
                        generationConfig = buildConfirmedGenerationConfig(inputConfig, sourceNode, mode, undefined, options?.confirmedModelKey);
                    } catch (error) {
                        reportGenerationDiagnostic("build-config-failed", { error: generationErrorMessage(error) });
                        message.error(generationErrorMessage(error));
                        return;
                    }
                    const hasLiveBatchChildren =
                        sourceNode?.type === CanvasNodeType.Image && (sourceNode.metadata?.batchChildIds || []).some((childId) => inputNodes.some((node) => node.id === childId && node.metadata?.batchRootId === sourceNode.id));
                    const hasStaleImageBatchState =
                        mode === "image" && sourceNode?.type === CanvasNodeType.Image && !sourceNode.metadata?.content && Boolean(sourceNode.metadata?.isBatchRoot || sourceNode.metadata?.batchChildIds?.length) && !hasLiveBatchChildren;
                    if (hasStaleImageBatchState) {
                        setNodesForOrigin((current) =>
                            current.map((node) => {
                                if (node.id !== sourceNode.id) return node;
                                const metadata = { ...node.metadata };
                                delete metadata.isBatchRoot;
                                delete metadata.batchChildIds;
                                delete metadata.primaryImageId;
                                delete metadata.imageBatchExpanded;
                                delete metadata.batchUsesReferenceImages;
                                return { ...node, metadata };
                            }),
                        );
                    }
                    if (!isAiConfigReady(generationConfig, generationConfig.model)) {
                        const diagChannel = resolveModelChannel(generationConfig, generationConfig.model);
                        reportGenerationDiagnostic("early-return-config-not-ready", {
                            model: generationConfig.model,
                            channelId: diagChannel.id,
                            channelName: diagChannel.name,
                            baseUrl: diagChannel.baseUrl,
                            apiKeyEmpty: !diagChannel.apiKey?.trim(),
                            hasApiKey: diagChannel.hasApiKey,
                            credentialRef: diagChannel.credentialRef,
                            taskWorkflowProvider: generationConfig.taskWorkflowProvider,
                        });
                        navigateToSettings({ continueCreation: true });
                        return;
                    }
                    reportGenerationDiagnostic("config-ready", { model: generationConfig.model });

                    const sourceTextContent = sourceNode?.type === CanvasNodeType.Text ? sourceNode.metadata?.content?.trim() || "" : "";
                    const editingTextNode = mode === "text" && Boolean(sourceTextContent);
                    let generationPrompt = mode === "image" && sourceNode?.metadata?.portraitTexture ? buildPortraitTexturePrompt(prompt, sourceNode.metadata.portraitTexture) : prompt;
                    if (mode === "image" && sourceNode?.metadata?.cameraControl?.enabled) {
                        const cameraControl = sourceNode.metadata.cameraControl;
                        const cameraPrompt = buildCameraPrompt({ cameraId: cameraControl.camera, lensId: cameraControl.lens, focalLengthMm: cameraControl.focalLength, apertureF: cameraControl.aperture });
                        generationPrompt = `${generationPrompt}\n${cameraPrompt}`;
                    }
                    const isPreparingEmptyImage = mode === "image" && sourceNode?.type === CanvasNodeType.Image && !sourceNode.metadata?.content;

                    let rawGenerationContext: Awaited<ReturnType<typeof hydrateNodeGenerationContext>>;
                    // AutoDL/其他声明式视频协议需要结构化参考素材；只有普通
                    // 模型视频接口才把提示词视为纯文本输入。
                    const requestConfig = resolveModelRequestConfig(generationConfig, generationConfig.model);
                    const usesWorkflowProvider = Boolean(
                        mode !== "text" &&
                            ((generationConfig.taskWorkflowProvider && generationConfig.taskWorkflowProvider !== "model") || protocolUsesStructuredMedia(requestConfig.interfaceType)),
                    );
                    // 普通视频协议只保留输入框文本（显式 @文本 引用仍会展开为真实内容）；声明式工作流还要保留连接媒体。
                    const promptOnly = mode === "video" && !usesWorkflowProvider;
                    try {
                        const baseContext = buildNodeGenerationContext(
                            nodeId,
                            inputNodes,
                            inputConnections,
                            editingTextNode ? buildTextRewritePrompt(sourceTextContent, prompt) : generationPrompt,
                            inputAssets,
                            promptOnly,
                        );
                        const requirements = generationModelRequirements(mode, baseContext, sourceNode, generationConfig, true);
                        generationConfig = buildConfirmedGenerationConfig(inputConfig, sourceNode, mode, requirements, options?.confirmedModelKey);
                        const compatibilityError = usesWorkflowProvider ? "" : modelCompatibilityError(generationConfig, generationConfig.model, requirements);
                        if (compatibilityError) throw new Error(`当前模型无法支持这组输入和参数：${compatibilityError}`);
                        const referenceLimits = usesWorkflowProvider ? undefined : modelGroupReferenceLimits(inputConfig, generationConfig.model, mode, requirements);
                        rawGenerationContext = await hydrateNodeGenerationContext(baseContext, projectId, domainProjectId, mode, mode === "video" && Boolean(referenceLimits?.maxAudios), !promptOnly, referenceLimits);
                        const hydratedRequirements = generationModelRequirements(mode, rawGenerationContext, sourceNode, generationConfig);
                        generationConfig = buildConfirmedGenerationConfig(inputConfig, sourceNode, mode, hydratedRequirements, options?.confirmedModelKey);
                        const hydratedCompatibilityError = usesWorkflowProvider ? "" : modelCompatibilityError(generationConfig, generationConfig.model, hydratedRequirements);
                        if (hydratedCompatibilityError) throw new Error(`当前模型无法支持这组输入和参数：${hydratedCompatibilityError}`);
                    } catch (error) {
                        reportGenerationDiagnostic("context-prepare-failed", { error: generationErrorMessage(error) });
                        const errorDetails = generationErrorMessage(error);
                        message.error(errorDetails);
                        return;
                    }
                    reportGenerationDiagnostic("context-ready", {
                        imageCount: rawGenerationContext.imageCount,
                        videoCount: rawGenerationContext.videoCount,
                        audioCount: rawGenerationContext.audioCount,
                        referenceImages: rawGenerationContext.referenceImages.length,
                        promptChars: rawGenerationContext.prompt.length,
                    });

                    let skillExecution: Awaited<ReturnType<typeof skillRuntime.prepare<"canvas">>>;
                    try {
                        reportGenerationDiagnostic("skill-prepare-start", { skillCount: inputSkills.length });
                        skillExecution = await skillRuntime.prepare({ profile: "canvas", prompt: rawGenerationContext.prompt, skills: inputSkills });
                        reportGenerationDiagnostic("skill-prepare-done", { promptChars: skillExecution.prompt.trim().length });
                    } catch (error) {
                        reportGenerationDiagnostic("skill-prepare-failed", { error: error instanceof Error ? error.message : String(error) });
                        message.error(error instanceof Error ? error.message : "技能上下文加载失败");
                        return;
                    }
                    let effectivePrompt = skillExecution.prompt.trim();
                    let styleMetadata = {};
                    if (mode === "image") {
                        try {
                            const styleRuntime = resolveCanvasStyleExecution(inputNodes, sourceNode, effectivePrompt, generationConfig, mode);
                            if (styleRuntime) {
                                effectivePrompt = styleRuntime.prompt;
                                styleMetadata = { styleProfileJson: styleRuntime.profileJson, styleExecutionPlan: styleRuntime.plan };
                            }
                        } catch (error) {
                            const errorDetails = generationErrorMessage(error);
                            message.error(errorDetails);
                            return;
                        }
                    }
                    const promptLengthError = mode === "video" ? modelPromptLengthError(generationConfig, generationConfig.model, mode, effectivePrompt) : "";
                    if (promptLengthError) {
                        reportGenerationDiagnostic("prompt-too-long", { error: promptLengthError, promptChars: effectivePrompt.length });
                        message.error(promptLengthError);
                        return;
                    }
                    const generationContext = { ...rawGenerationContext, prompt: effectivePrompt };
                    if ((options?.retryContext || sourceNode?.metadata?.failedInputFingerprint || sourceNode?.metadata?.failedPromptFingerprint) && canvasGenerationRetryBlocked(sourceNode?.metadata, { ...generationContext, mode })) {
                        message.warning(sourceNode?.metadata?.errorDetails || "请先查看失败原因并调整输入，再重新生成");
                        return;
                    }
                    if (mode === "audio" && generationContext.characterReferences.length) {
                        if (generationContext.characterReferences.length !== 1) {
                            message.error("角色配音一次只能引用一个角色卡");
                            return;
                        }
                        const voice = generationContext.resolvedCharacterVoices[0];
                        if (!voice) {
                            message.error("角色尚未绑定可用声音，无法创建角色配音任务");
                            return;
                        }
                        generationConfig = { ...generationConfig, audioVoice: voice.voiceKey, audioInstructions: [voice.instructions, generationConfig.audioInstructions].filter(Boolean).join("；") };
                    }
                    if (!effectivePrompt && (mode === "text" || mode === "audio")) {
                        return;
                    }

                    const requestFingerprint = canvasGenerationRequestFingerprint({
                        nodeId,
                        mode,
                        prompt: effectivePrompt,
                        model: generationConfig.model,
                        options: modelRequestOptions(generationConfig, mode),
                        workflow:
                            generationConfig.taskWorkflowProvider && generationConfig.taskWorkflowProvider !== "model"
                                ? {
                                      provider: generationConfig.taskWorkflowProvider,
                                      workflowId: generationConfig.runningHub.workflowId,
                                      workflowKind: generationConfig.runningHub.selectedKind,
                                      parameters: sourceNode?.metadata?.workflowParameters,
                                  }
                                : undefined,
                        operation: sourceNode?.metadata?.videoEditOperation,
                        audioInstructions: generationConfig.audioInstructions,
                        promptTemplateOperation: sourceNode?.metadata?.promptTemplateOperation,
                        promptTemplateVariables: sourceNode?.metadata?.promptTemplateVariables,
                        context: generationContext,
                    });
                    const duplicateConfirmationRequired = !options?.skipDuplicateConfirmation && !options?.retryContext && sourceNode?.metadata?.lastGenerationRequestFingerprint === requestFingerprint;
                    reportGenerationDiagnostic("pre-submit", {
                        duplicateConfirmationRequired,
                        usesWorkflowProvider,
                        promptChars: effectivePrompt.length,
                        referenceImages: generationContext.referenceImages.length,
                    });
                    if (duplicateConfirmationRequired && !(await confirmDuplicateSubmission())) return;

                    const ratioWarning = mode === "video" && !usesWorkflowProvider ? seedanceReferenceRatioWarning({
                        ...resolveModelRequestConfig(generationConfig, generationConfig.model),
                        videoCount: generationContext.referenceVideos.length,
                        ratio: generationConfig.size,
                        operation: sourceNode?.metadata?.videoEditOperation,
                    }) : undefined;
                    if (ratioWarning) {
                        const accepted = await new Promise<boolean>((resolve) => {
                            modal.confirm({ ...ratioWarning, centered: true, onOk: () => resolve(true), onCancel: () => resolve(false), afterClose: () => resolve(false) });
                        });
                        if (!accepted || options?.controller?.signal.aborted) return;
                    }

                    setRunningNodeId(nodeId);
                    const controller = startGenerationRequest(nodeId, nodeId, nodeId, options?.controller);
                    if (controller.signal.aborted) {
                        finishGenerationRequest(nodeId, controller);
                        setRunningNodeId(null);
                        return;
                    }
                    if (isPreparingEmptyImage) {
                        setNodesForOrigin((current) =>
                            current.map((node) =>
                                node.id === nodeId
                                    ? {
                                          ...node,
                                          metadata: {
                                              ...node.metadata,
                                              prompt,
                                              composerContent: prompt,
                                              status: NODE_STATUS_LOADING,
                                              taskStage: "正在准备生成任务",
                                              taskProgress: 0,
                                              taskCreatedAt: new Date().toISOString(),
                                              errorDetails: undefined,
                                              generationErrorCode: undefined,
                                              resourceReloadAvailable: undefined,
                                              failedPromptFingerprint: undefined,
                                              failedInputFingerprint: undefined,
                                          },
                                      }
                                    : node,
                            ),
                        );
                    }

                    // 已有内容节点只是本次生成的来源；任务状态归新目标所有，不能覆盖已成功结果。
                    const markSourceStatus = !sourceNode?.metadata?.content && !editingTextNode;
                    const hasPreviousResult = Boolean(sourceNode?.metadata?.content || sourceNode?.metadata?.storageKey);
                    const statusPrompt = sourceNode?.type === CanvasNodeType.Config ? effectivePrompt : prompt;
                    if (markSourceStatus)
                        setNodesForOrigin((current) =>
                            current.map((node) =>
                                node.id === nodeId
                                    ? {
                                          ...node,
                                          metadata: { ...node.metadata, ...canvasGenerationPromptMetadata(prompt, statusPrompt), status: NODE_STATUS_LOADING, errorDetails: undefined, generationErrorCode: undefined, resourceReloadAvailable: undefined, failedPromptFingerprint: undefined, failedInputFingerprint: undefined },
                                      }
                                    : node,
                            ),
                        );

                    let pendingNodeIds: string[] = [];
                    const execution = {
                        projectId,
                        nodeId,
                        sourceNode,
                        canvasNodes: inputNodes,
                        canvasConnections: inputConnections,
                        prompt,
                        effectivePrompt,
                        generationConfig,
                        generationContext,
                        controller,
                        editingTextNode,
                        styleMetadata,
                        skillMetadata: skillExecution.metadata,
                        taskContext: options?.context,
                        retryContext: options?.retryContext,
                        clientOperationId: options?.clientOperationId,
                        setNodes: setNodesForOrigin,
                        setConnections: setConnectionsForOrigin,
                        setSelectedNodeIds,
                        setSelectedConnectionId,
                        setDialogNodeId,
                        startGenerationRequest,
                        finishGenerationRequest,
                        bindGenerationTask: (targetNodeId: string, task: GenerationTask) => {
                            reportGenerationDiagnostic("task-created", {
                                targetNodeId,
                                taskId: task.id,
                                taskType: task.type,
                                taskStatus: task.status,
                                provider: task.provider,
                            });
                            bindGenerationTask(targetNodeId, task);
                            setNodesForOrigin((current) => {
                                const source = current.find((node) => node.id === nodeId);
                                if (!source || source.metadata?.lastGenerationRequestFingerprint === requestFingerprint) return current;
                                return current.map((node) => (node.id === nodeId ? { ...node, metadata: { ...node.metadata, lastGenerationRequestFingerprint: requestFingerprint } } : node));
                            });
                            options?.onTaskUpdate?.(task);
                        },
                        applyGenerationTaskResult,
                        showError: (content: string) => message.error(content),
                        resolveReferenceLinks: createReferenceLinkResolver(modal),
                        registerPendingNodeIds: (nodeIds: string[]) => {
                            pendingNodeIds = nodeIds;
                        },
                    };

                    try {
                        reportGenerationDiagnostic("execute-start", { mode, usesWorkflowProvider, hasExecution: Boolean(execution) });
                        if (mode === "image") await executeImageGeneration(execution);
                        else if (mode === "video") await executeVideoGeneration(execution);
                        else if (mode === "audio") await executeAudioGeneration(execution);
                        else await executeTextGeneration(execution);
                        reportGenerationDiagnostic("execute-finished", { mode });
                    } catch (error) {
                        if (isGenerationCanceled(error)) return;
                        const failure = canvasGenerationFailureMetadata(error, { ...generationContext, mode });
                        reportGenerationDiagnostic("execute-failed", {
                            mode,
                            error: generationErrorMessage(error),
                            raw: error instanceof Error ? `${error.name}: ${error.message}` : String(error),
                        });
                        if (options?.waitForTaskCapacity && isGenerationTaskCapacityError(error)) {
                            setNodesForOrigin((current) =>
                                current.map((node) => {
                                    if (node.id !== nodeId && !pendingNodeIds.includes(node.id)) return node;
                                    const metadata = { ...(node.metadata || {}), status: NODE_STATUS_IDLE, errorDetails: undefined };
                                    delete metadata.taskId;
                                    delete metadata.taskStatus;
                                    delete metadata.taskProgress;
                                    delete metadata.taskStage;
                                    delete metadata.taskCreatedAt;
                                    delete metadata.taskUpdatedAt;
                                    delete metadata.taskStartedAt;
                                    delete metadata.taskCompletedAt;
                                    delete metadata.taskDurationMs;
                                    return { ...node, metadata };
                                }),
                            );
                            return;
                        }
                        message.error(failure.errorDetails);
                        setNodesForOrigin((current) =>
                            current.map((node) => {
                                if (node.id !== nodeId && !pendingNodeIds.includes(node.id)) return node;
                                if (node.id === nodeId && hasPreviousResult) {
                                    return { ...node, metadata: { ...node.metadata, status: NODE_STATUS_SUCCESS, errorDetails: undefined, generationErrorCode: undefined, failedPromptFingerprint: undefined, failedInputFingerprint: undefined } };
                                }
                                if (node.id === nodeId && !markSourceStatus) return node;
                                return { ...node, metadata: { ...node.metadata, status: NODE_STATUS_ERROR, ...failure } };
                            }),
                        );
                    } finally {
                        finishGenerationRequest(nodeId, controller);
                        setRunningNodeId(null);
                    }
                },
                () => message.info({ key: `canvas-generation-submission-${nodeId}`, content: "该节点已有生成请求正在提交或执行，请勿重复点击" }),
            ),
        [
            addedSkills,
            applyGenerationTaskResult,
            bindGenerationTask,
            confirmDuplicateSubmission,
            domainProjectId,
            effectiveConfig,
            finishGenerationRequest,
            isAiConfigReady,
            message,
            modal,
            nodesRef,
            connectionsRef,
            projectId,
            setConnections,
            setDialogNodeId,
            setNodes,
            setRunningNodeId,
            setSelectedConnectionId,
            setSelectedNodeIds,
            startGenerationRequest,
        ],
    );
}


function generationModelRequirements(
    mode: CanvasNodeGenerationMode,
    input: Pick<Awaited<ReturnType<typeof hydrateNodeGenerationContext>>, "textCount" | "imageCount" | "videoCount" | "audioCount" | "characterReferences">,
    sourceNode: CanvasNodeData | undefined,
    config: ReturnType<typeof useEffectiveConfig>,
    includeCharacterMinimum = false,
): ModelRequirements {
    return {
        capability: mode,
        input: {
            textCount: input.textCount,
            imageCount: input.imageCount,
            videoCount: input.videoCount,
            audioCount: input.audioCount,
            characterCount: includeCharacterMinimum ? input.characterReferences.length : 0,
        },
        videoOperation: sourceNode?.metadata?.videoEditOperation,
        videoSeconds: config.videoSeconds,
        options: config.taskWorkflowProvider === "model" ? modelRequestOptions(config, mode) : undefined,
    };
}
