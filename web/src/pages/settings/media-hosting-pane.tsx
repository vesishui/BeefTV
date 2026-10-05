/**
 * 素材图床设置面板。
 *
 * 启用后，本地参考素材会先上传图床换成公网地址再发给上游渠道，解决
 * dashscope-wan-video / minimax-video 这类只收 URL 的协议无法使用本地素材的问题。
 * 图片、视频、音频共用同一套配置。
 */
import { App, Button, Form, Input, Select } from "antd";
import { AlertTriangle, CheckCircle2, UploadCloud } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import {
    MEDIA_HOSTING_PROVIDER_LABELS,
    MEDIA_HOSTING_REDACTED,
    getMediaHostingConfig,
    saveMediaHostingConfig,
    testMediaHosting,
    type MediaHostingConfig,
    type MediaHostingKind,
    type MediaHostingProvider,
} from "@/services/api/media-hosting";

import "./media-hosting-pane.css";

/** 面板里可选的全部图床类型，顺序按常用度排列。 */
const PROVIDER_ORDER: MediaHostingProvider[] = ["none", "builtin", "free", "custom", "aliyun-oss", "tencent-cos", "cloudflare-r2", "aws-s3"];

const PROVIDER_HINTS: Record<MediaHostingProvider, string> = {
    none: "保持原有行为，参考素材按渠道协议直接发送。",
    builtin: "用本机 BeefTV 生成带签名的临时链接，需要配置服务器公开访问地址。",
    free: "上传到公共免费图床。素材会离开本机，请勿用于私密内容。",
    custom: "上传到你自己的接口，返回体里包含公网 URL 即可。",
    "aliyun-oss": "上传到阿里云对象存储，链接长期有效。",
    "tencent-cos": "上传到腾讯云对象存储，链接长期有效。",
    "cloudflare-r2": "上传到 Cloudflare R2，没有出站流量费用。",
    "aws-s3": "上传到 AWS S3。",
};

const FREE_PROVIDERS = [
    { value: "auto", label: "自动选择" },
    { value: "litterbox", label: "Litterbox（72 小时）" },
    { value: "uguu", label: "Uguu（支持音频）" },
    { value: "imgbb", label: "ImgBB（需 API Key）" },
];

const FREE_EXPIRY_OPTIONS = [
    { value: "1h", label: "1 小时" },
    { value: "12h", label: "12 小时" },
    { value: "24h", label: "24 小时" },
    { value: "72h", label: "72 小时" },
];

const TEST_KINDS: Array<{ value: MediaHostingKind; label: string }> = [
    { value: "image", label: "图片" },
    { value: "audio", label: "音频" },
    { value: "video", label: "视频" },
];

type SaveState = { status: "idle" | "saving" | "saved" | "error"; message?: string };
type TestState = { status: "idle" | "testing" | "ok" | "error"; url?: string; message?: string };

export function MediaHostingPane() {
    const { message } = App.useApp();
    const [draft, setDraft] = useState<MediaHostingConfig>({ provider: "none" });
    const [cachedCount, setCachedCount] = useState(0);
    const [loading, setLoading] = useState(true);
    const [saveState, setSaveState] = useState<SaveState>({ status: "idle" });
    const [testState, setTestState] = useState<TestState>({ status: "idle" });
    const [testKind, setTestKind] = useState<MediaHostingKind>("image");

    useEffect(() => {
        let cancelled = false;
        void (async () => {
            try {
                const result = await getMediaHostingConfig();
                if (cancelled) return;
                setDraft(result.config);
                setCachedCount(result.cachedCount ?? 0);
            } catch (error) {
                if (!cancelled) {
                    setSaveState({ status: "error", message: error instanceof Error ? error.message : "读取图床配置失败" });
                }
            } finally {
                if (!cancelled) setLoading(false);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, []);

    const patch = useCallback((next: Partial<MediaHostingConfig>) => {
        setDraft((current) => ({ ...current, ...next }));
        setSaveState({ status: "idle" });
        setTestState({ status: "idle" });
    }, []);

    const save = useCallback(async () => {
        setSaveState({ status: "saving" });
        try {
            const result = await saveMediaHostingConfig(draft);
            setDraft(result.config);
            setSaveState({ status: "saved" });
            message.success(result.enabled ? "图床已启用" : "图床已关闭");
        } catch (error) {
            const text = error instanceof Error ? error.message : "保存图床配置失败";
            setSaveState({ status: "error", message: text });
            message.error(text);
        }
    }, [draft, message]);

    const runTest = useCallback(async () => {
        setTestState({ status: "testing" });
        try {
            const result = await testMediaHosting(testKind);
            setTestState({ status: "ok", url: result.url });
        } catch (error) {
            setTestState({ status: "error", message: error instanceof Error ? error.message : "图床上传测试失败" });
        }
    }, [testKind]);

    const provider = draft.provider ?? "none";
    const showsSecretPlaceholder = (value?: string) => value === MEDIA_HOSTING_REDACTED;

    if (loading) {
        return <div className="settings-section">正在读取图床配置…</div>;
    }

    return (
        <div className="settings-section">
            <div className="settings-pane-header">
                <div>
                    <div className="eyebrow">Media Hosting</div>
                    <h2>素材图床</h2>
                </div>
                <div className="settings-pane-header-actions">
                    {saveState.status === "saved" && (
                        <span className="media-hosting-badge is-ok">
                            <CheckCircle2 className="size-4" /> 已保存
                        </span>
                    )}
                    <Button type="primary" loading={saveState.status === "saving"} onClick={() => void save()}>
                        保存
                    </Button>
                </div>
            </div>

            <p className="media-hosting-description">
                启用后，参考素材会先上传到图床并换成公网链接再提交给模型渠道。适合只接受在线素材地址的渠道。
            </p>

            <Form layout="vertical" className="media-hosting-form">
                <Form.Item label="图床类型">
                    <Select
                        aria-label="图床类型"
                        value={provider}
                        onChange={(value) => patch({ provider: value as MediaHostingProvider })}
                        options={PROVIDER_ORDER.map((value) => ({ value, label: MEDIA_HOSTING_PROVIDER_LABELS[value] }))}
                    />
                    <p className="media-hosting-hint">{PROVIDER_HINTS[provider]}</p>
                </Form.Item>

                {provider === "free" && (
                    <>
                        <Form.Item label="免费图床">
                            <Select
                                aria-label="免费图床"
                                value={draft.freeProvider ?? "auto"}
                                onChange={(value) => patch({ freeProvider: value as MediaHostingConfig["freeProvider"] })}
                                options={FREE_PROVIDERS}
                            />
                        </Form.Item>
                        {(draft.freeProvider ?? "auto") === "litterbox" && (
                            <Form.Item label="有效期">
                                <Select
                                    aria-label="有效期"
                                    value={draft.freeExpiry ?? "24h"}
                                    onChange={(value) => patch({ freeExpiry: value })}
                                    options={FREE_EXPIRY_OPTIONS}
                                />
                            </Form.Item>
                        )}
                        {(draft.freeProvider ?? "auto") === "imgbb" && (
                            <Form.Item label="ImgBB API Key">
                                <Input.Password
                                    aria-label="ImgBB API Key"
                                    autoComplete="new-password"
                                    placeholder={showsSecretPlaceholder(draft.imgbbApiKey) ? "已保存，留空表示不修改" : "粘贴 ImgBB API Key"}
                                    value={showsSecretPlaceholder(draft.imgbbApiKey) ? "" : draft.imgbbApiKey ?? ""}
                                    onChange={(event) => patch({ imgbbApiKey: event.target.value })}
                                />
                            </Form.Item>
                        )}
                        <div className="media-hosting-notice">
                            <AlertTriangle className="size-4" />
                            <span>免费公共图床会把素材上传到第三方服务，请勿用于私密或敏感素材。免费图床不收视频。</span>
                        </div>
                    </>
                )}

                {provider === "custom" && (
                    <>
                        <Form.Item label="上传地址">
                            <Input
                                aria-label="上传地址"
                                placeholder="https://example.com/api/upload"
                                value={draft.endpoint ?? ""}
                                onChange={(event) => patch({ endpoint: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="文件字段名">
                            <Input
                                aria-label="文件字段名"
                                placeholder="file"
                                value={draft.customFileField ?? ""}
                                onChange={(event) => patch({ customFileField: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="返回 URL 路径">
                            <Input
                                aria-label="返回 URL 路径"
                                placeholder="data.url（留空时自动识别）"
                                value={draft.customUrlPath ?? ""}
                                onChange={(event) => patch({ customUrlPath: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="鉴权 Header">
                            <Input
                                aria-label="鉴权 Header"
                                placeholder="Authorization"
                                value={draft.customAuthHeader ?? ""}
                                onChange={(event) => patch({ customAuthHeader: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="鉴权前缀">
                            <Input
                                aria-label="鉴权前缀"
                                placeholder="Bearer"
                                value={draft.customAuthScheme ?? ""}
                                onChange={(event) => patch({ customAuthScheme: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="Token">
                            <Input.Password
                                aria-label="Token"
                                autoComplete="new-password"
                                placeholder={showsSecretPlaceholder(draft.customToken) ? "已保存，留空表示不修改" : "可选"}
                                value={showsSecretPlaceholder(draft.customToken) ? "" : draft.customToken ?? ""}
                                onChange={(event) => patch({ customToken: event.target.value })}
                            />
                        </Form.Item>
                    </>
                )}

                {(provider === "aliyun-oss" || provider === "tencent-cos" || provider === "cloudflare-r2" || provider === "aws-s3") && (
                    <>
                        <Form.Item label="存储桶">
                            <Input aria-label="存储桶" value={draft.bucket ?? ""} onChange={(event) => patch({ bucket: event.target.value })} />
                        </Form.Item>
                        {provider === "cloudflare-r2" ? (
                            <Form.Item label="Account ID">
                                <Input aria-label="Account ID" value={draft.accountId ?? ""} onChange={(event) => patch({ accountId: event.target.value })} />
                            </Form.Item>
                        ) : (
                            <Form.Item label="区域">
                                <Input aria-label="区域" value={draft.region ?? ""} onChange={(event) => patch({ region: event.target.value })} />
                            </Form.Item>
                        )}
                        <Form.Item label="Access Key ID">
                            <Input
                                aria-label="Access Key ID"
                                autoComplete="off"
                                placeholder={showsSecretPlaceholder(draft.accessKeyId) ? "已保存，留空表示不修改" : ""}
                                value={showsSecretPlaceholder(draft.accessKeyId) ? "" : draft.accessKeyId ?? ""}
                                onChange={(event) => patch({ accessKeyId: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="Secret Access Key">
                            <Input.Password
                                aria-label="Secret Access Key"
                                autoComplete="new-password"
                                placeholder={showsSecretPlaceholder(draft.secretAccessKey) ? "已保存，留空表示不修改" : ""}
                                value={showsSecretPlaceholder(draft.secretAccessKey) ? "" : draft.secretAccessKey ?? ""}
                                onChange={(event) => patch({ secretAccessKey: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="公开访问地址">
                            <Input
                                aria-label="公开访问地址"
                                placeholder="https://cdn.example.com"
                                value={draft.publicBaseUrl ?? ""}
                                onChange={(event) => patch({ publicBaseUrl: event.target.value })}
                            />
                        </Form.Item>
                        <Form.Item label="路径前缀">
                            <Input aria-label="路径前缀" value={draft.pathPrefix ?? ""} onChange={(event) => patch({ pathPrefix: event.target.value })} />
                        </Form.Item>
                    </>
                )}

                {provider === "builtin" && (
                    <div className="media-hosting-notice">
                        <AlertTriangle className="size-4" />
                        <span>内置图床使用本机 BeefTV 的签名链接，需要在服务器上配置公开访问地址（CANVAS_PUBLIC_BASE_URL），否则无法生成链接。</span>
                    </div>
                )}

                {provider !== "none" && (
                    <div className="media-hosting-test">
                        <Select
                            aria-label="测试素材类型"
                            value={testKind}
                            onChange={(value) => setTestKind(value as MediaHostingKind)}
                            options={TEST_KINDS}
                        />
                        <Button icon={<UploadCloud className="size-4" />} loading={testState.status === "testing"} onClick={() => void runTest()}>
                            测试上传
                        </Button>
                        {testState.status === "ok" && (
                            <span className="media-hosting-badge is-ok">
                                <CheckCircle2 className="size-4" /> 上传成功
                            </span>
                        )}
                        {testState.status === "error" && <span className="media-hosting-badge is-error">{testState.message}</span>}
                    </div>
                )}

                {testState.status === "ok" && testState.url && <p className="media-hosting-hint">测试链接：{testState.url}</p>}
                {saveState.status === "error" && saveState.message && <p className="media-hosting-hint is-error">{saveState.message}</p>}
                {cachedCount > 0 && <p className="media-hosting-hint">已缓存 {cachedCount} 个素材链接，重复素材不会重复上传。</p>}
            </Form>
        </div>
    );
}
