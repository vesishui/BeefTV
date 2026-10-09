import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import { bootstrapAppearance } from "@/services/appearance-bootstrap";
import { bootstrapDesktopRuntime } from "@/services/desktop-runtime";
import { hydrateProtocolCapabilities } from "@/services/api/protocol-capabilities";
import { hydrateLocalCanvasProjectsFromBackend } from "@/services/local-workspace-repository";
import { useCanvasStore } from "@/stores/canvas/use-canvas-store";

async function startApplication() {
    await bootstrapDesktopRuntime();
    // 浏览器缓存晚于后端读入完成时，会用旧连接数组盖掉刚合并的画布。
    await useCanvasStore.persist.rehydrate();
    // 协议标记决定声明式视频协议是否保留参考素材，必须在打开画布前就绪。
    await hydrateProtocolCapabilities();
    await hydrateLocalCanvasProjectsFromBackend();
    await bootstrapAppearance().finally(() => import("./application"));
}

void startApplication();
